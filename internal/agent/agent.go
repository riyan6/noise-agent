package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	randv2 "math/rand/v2"
	"noise-agent/internal/client"
	"noise-agent/internal/collector"
	"noise-agent/internal/model"
	"noise-agent/internal/probe"
	"time"
)

func randomID() string {
	var value [16]byte
	_, _ = rand.Read(value[:])
	return hex.EncodeToString(value[:])
}

type Options struct {
	Endpoint    string
	Token       string
	MonthRotate int
	Logger      *slog.Logger
}
type Agent struct {
	client      *client.Client
	collector   *collector.Collector
	prober      *probe.Manager
	logger      *slog.Logger
	bootID      string
	sequence    int64
	config      model.AgentConfig
	lastInfo    model.AgentInfo
	hasSentInfo bool
}

func New(opts Options) *Agent {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Agent{client: client.New(opts.Endpoint, opts.Token), collector: collector.New(), prober: probe.NewManager(), logger: logger, bootID: randomID(),
		config: model.AgentConfig{Refresh: 3, ArchiveSeconds: 300, GeoIP: model.GeoIPConfig{Enabled: true, Provider: "ipinfo"}}}
}

func (a *Agent) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer a.prober.StopAll()
	outgoing := make(chan model.ClientMessage, 8)
	incoming := make(chan model.ServerMessage, 16)
	go a.connections(ctx, outgoing, incoming)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	infoTicker := time.NewTicker(30 * time.Minute)
	defer infoTicker.Stop()
	history := newBuffer(a.bootID)
	var latest model.Report
	var connected, infoBusy bool
	var waiting *model.Archive
	var sentAt, infoRetryAt time.Time
	type infoResult struct {
		info model.AgentInfo
		err  error
	}
	infoDone := make(chan infoResult, 1)
	liveLatency := map[string]model.LatencyResult{}
	send := func(message model.ClientMessage) bool {
		if !connected {
			return false
		}
		select {
		case outgoing <- message:
			return true
		default:
			return false
		}
	}
	syncInfo := func() {
		if infoBusy || time.Now().Before(infoRetryAt) {
			return
		}
		infoBusy = true
		geo := a.config.GeoIP
		go func() {
			info := a.collector.CollectInfo(ctx, geo)
			err := a.client.PostInfo(ctx, info)
			select {
			case infoDone <- infoResult{info, err}:
			case <-ctx.Done():
			}
		}()
	}
	collect := func() {
		latest = a.collector.Sample(a.bootID, a.sequence)
		latest.Time = time.Now().UnixMilli()
		a.sequence++
		history.sample(latest)
	}
	sendReport := func() {
		results := make([]model.LatencyResult, 0, len(liveLatency))
		for _, result := range liveLatency {
			results = append(results, result)
		}
		report := latest
		if send(model.ClientMessage{Type: "report", Report: &report, Results: results}) {
			clear(liveLatency)
		}
	}
	sendArchive := func() {
		if !connected {
			return
		}
		if waiting != nil && time.Since(sentAt) < 15*time.Second {
			return
		}
		chunk := history.next(time.Now().UnixMilli())
		if chunk == nil {
			waiting = nil
			return
		}
		sealed := *chunk
		sealed.Samples = append([]model.PackedSample{}, chunk.Samples...)
		sealed.Latency = append([]model.LatencyResult{}, chunk.Latency...)
		if send(model.ClientMessage{Type: "archive", Archive: &sealed}) {
			waiting = chunk
			sentAt = time.Now()
		}
	}
	collect()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			collect()
			sendReport()
			sendArchive()
			if connected && !a.hasSentInfo {
				syncInfo()
			}
		case <-a.prober.Ready():
			results := a.prober.Drain(1000)
			history.latency(results)
			for _, result := range results {
				liveLatency[result.TaskID] = result
			}
		case <-infoTicker.C:
			if connected {
				syncInfo()
			}
		case result := <-infoDone:
			infoBusy = false
			if result.err != nil {
				infoRetryAt = time.Now().Add(time.Minute)
				a.logger.Warn("hardware report failed; retrying later", "err", result.err)
			} else {
				a.lastInfo = result.info
				a.hasSentInfo = true
			}
		case message := <-incoming:
			switch message.Type {
			case "connected":
				connected = true
				waiting = nil
				ticker.Reset(time.Duration(a.config.Refresh) * time.Second)
				sendReport()
				sendArchive()
			case "disconnected":
				connected = false
				waiting = nil
				for len(outgoing) > 0 {
					<-outgoing
				}
			case "config":
				previousGeo := a.config.GeoIP
				a.config = message.Config
				a.config.Refresh = clampRefresh(a.config.Refresh)
				ticker.Reset(time.Duration(a.config.Refresh) * time.Second)
				for i := range a.config.Tasks {
					if a.config.Tasks[i].Interval < 30 {
						a.config.Tasks[i].Interval = 30
					}
				}
				a.prober.SyncTasks(ctx, a.config.Tasks)
				if !a.hasSentInfo || previousGeo != a.config.GeoIP {
					syncInfo()
				}
			case "archiveAck":
				history.ack(message.BootID, message.Start)
				waiting = nil
				sendArchive()
			case "error":
				a.logger.Warn("worker rejected message", "message", message.Message)
			}
		}
	}
}

func (a *Agent) connections(ctx context.Context, outgoing <-chan model.ClientMessage, incoming chan<- model.ServerMessage) {
	delay := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := a.client.Stream(ctx, outgoing, incoming)
		if ctx.Err() != nil {
			return
		}
		select {
		case incoming <- model.ServerMessage{Type: "disconnected"}:
		case <-ctx.Done():
			return
		}
		if time.Since(started) > time.Minute {
			delay = time.Second
		}
		if errors.Is(err, client.ErrUnauthorized) {
			delay = 5 * time.Minute
		}
		a.logger.Warn("worker connection lost", "retryIn", delay, "err", err)
		wait := time.NewTimer(delay + time.Duration(randv2.Int64N(int64(delay/5+1))))
		select {
		case <-ctx.Done():
			wait.Stop()
			return
		case <-wait.C:
		}
		if delay < time.Minute {
			delay = min(delay*2, time.Minute)
		}
	}
}

func clampRefresh(seconds int) int { return max(3, min(seconds, 10)) }
