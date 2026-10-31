package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"noise-agent/internal/client"
	"noise-agent/internal/collector"
	"noise-agent/internal/model"
	"noise-agent/internal/probe"
)

func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
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
	pending     *model.Report
	config      model.AgentConfig
	lastInfo    model.AgentInfo
	hasSentInfo bool
}

func New(opts Options) *Agent {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Agent{
		client:    client.New(opts.Endpoint, opts.Token),
		collector: collector.New(),
		prober:    probe.NewManager(),
		logger:    logger,
		bootID:    randomID(),
		config: model.AgentConfig{
			Refresh: 1,
			GeoIP:   model.GeoIPConfig{Enabled: true, Provider: "ipinfo"},
		},
	}
}

func (a *Agent) Run(ctx context.Context) error {
	defer a.prober.StopAll()

	a.syncConfigAndInfo(ctx, true)

	refreshDur := time.Duration(clampRefresh(a.config.Refresh)) * time.Second
	reportTicker := time.NewTicker(refreshDur)
	defer reportTicker.Stop()

	configTicker := time.NewTicker(60 * time.Second)
	defer configTicker.Stop()

	infoTicker := time.NewTicker(30 * time.Minute)
	defer infoTicker.Stop()

	a.sendReport(ctx, reportTicker, &refreshDur)

	for {
		select {
		case <-ctx.Done():
			a.flushLatency(context.Background())
			return nil
		case <-reportTicker.C:
			if !a.hasSentInfo {
				a.syncInfo(ctx)
			}
			a.sendReport(ctx, reportTicker, &refreshDur)
			a.flushLatency(ctx)
		case <-a.prober.Ready():
			a.flushLatency(ctx)
		case <-configTicker.C:
			a.syncConfigAndInfo(ctx, false)
			a.updateTicker(reportTicker, &refreshDur)
		case <-infoTicker.C:
			a.syncInfo(ctx)
		}
	}
}

func (a *Agent) updateTicker(ticker *time.Ticker, current *time.Duration) {
	desired := time.Duration(clampRefresh(a.config.Refresh)) * time.Second
	if desired != *current {
		*current = desired
		ticker.Reset(desired)
	}
}

func (a *Agent) syncConfigAndInfo(ctx context.Context, initial bool) {
	cfg, err := a.client.GetConfig(ctx)
	if err != nil {
		a.logger.Warn("failed to fetch agent config", "err", err)
		if initial {
			a.syncInfo(ctx)
		}
		return
	}
	prevGeo := a.config.GeoIP
	prevVer := a.config.Version
	a.config = cfg

	if initial || prevVer != cfg.Version {
		a.prober.SyncTasks(ctx, cfg.Tasks)
		a.logger.Info("synchronized agent config", "version", cfg.Version, "refresh", cfg.Refresh, "tasks", len(cfg.Tasks))
	}

	if initial || !a.hasSentInfo || prevGeo != cfg.GeoIP {
		a.syncInfo(ctx)
	}
}

func (a *Agent) syncInfo(ctx context.Context) {
	info := a.collector.CollectInfo(ctx, a.config.GeoIP)
	if a.hasSentInfo && info == a.lastInfo {
		return
	}
	if err := a.client.PostInfo(ctx, info); err != nil {
		a.logger.Warn("failed to report node info", "err", err)
		return
	}
	a.lastInfo = info
	a.hasSentInfo = true
	a.logger.Info("reported node info", "ip", info.IP, "region", info.Region, "city", info.City, "os", info.OS, "cores", info.Cores)
}

func (a *Agent) sendReport(ctx context.Context, ticker *time.Ticker, currentRefresh *time.Duration) {
	if a.pending == nil {
		sample := a.collector.Sample(a.bootID, a.sequence)
		a.pending = &sample
	}

	ack, err := a.client.PostReport(ctx, *a.pending)
	if err != nil {
		if errors.Is(err, client.ErrConflict) {
			a.logger.Warn("report conflict detected, rotating bootId")
			a.bootID = randomID()
			a.sequence = 0
			a.pending = nil
			return
		}
		a.logger.Warn("failed to send metric report", "sequence", a.pending.Sequence, "err", err)
		return
	}

	a.sequence++
	a.pending = nil

	if ack.ConfigVersion != "" && ack.ConfigVersion != a.config.Version {
		a.syncConfigAndInfo(ctx, false)
		a.updateTicker(ticker, currentRefresh)
	}
}

func (a *Agent) flushLatency(ctx context.Context) {
	for {
		batch := a.prober.Drain(1000)
		if len(batch) == 0 {
			return
		}
		err := a.client.PostLatencyResults(ctx, batch)
		if err != nil {
			if errors.Is(err, client.ErrBadRequest) {
				// Unbound task or stale task list; reload config and drop invalid batch.
				a.logger.Warn("latency batch rejected by server, refreshing config", "err", err)
				a.syncConfigAndInfo(ctx, false)
				return
			}
			a.logger.Warn("failed to upload latency results, requeueing", "count", len(batch), "err", err)
			a.prober.Requeue(batch)
			return
		}
	}
}

func clampRefresh(seconds int) int {
	switch seconds {
	case 1, 2, 5, 10:
		return seconds
	default:
		if seconds < 1 {
			return 1
		}
		if seconds > 10 {
			return 10
		}
		return seconds
	}
}
