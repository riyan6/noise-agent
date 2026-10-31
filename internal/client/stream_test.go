package client

import (
	"context"
	"errors"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"net/http"
	"net/http/httptest"
	"noise-agent/internal/model"
	"os"
	"testing"
	"time"
)

func TestStreamAuthenticatesAndTransfersConfigReportsAndArchiveAcks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/connect" || r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			serverErrors <- err
			return
		}
		defer conn.CloseNow()
		if err = wsjson.Write(ctx, conn, model.ServerMessage{Type: "config", Config: model.AgentConfig{Refresh: 3, ArchiveSeconds: 300}}); err != nil {
			serverErrors <- err
			return
		}
		var report model.ClientMessage
		if err = wsjson.Read(ctx, conn, &report); err != nil {
			serverErrors <- err
			return
		}
		if report.Type != "report" || report.Report == nil || report.Report.CPU != 25 {
			serverErrors <- errors.New("invalid report payload")
			return
		}
		if err = wsjson.Write(ctx, conn, model.ServerMessage{Type: "reportAck", Sequence: report.Report.Sequence}); err != nil {
			serverErrors <- err
			return
		}
		var archive model.ClientMessage
		if err = wsjson.Read(ctx, conn, &archive); err != nil {
			serverErrors <- err
			return
		}
		if archive.Archive == nil || len(archive.Archive.Samples) != 1 {
			serverErrors <- errors.New("invalid archive payload")
			return
		}
		err = wsjson.Write(ctx, conn, model.ServerMessage{Type: "archiveAck", BootID: archive.Archive.BootID, Start: archive.Archive.Start})
		serverErrors <- err
		<-ctx.Done()
	}))
	defer server.Close()
	outgoing := make(chan model.ClientMessage, 2)
	incoming := make(chan model.ServerMessage, 8)
	done := make(chan error, 1)
	go func() { done <- New(server.URL, "test-token").Stream(ctx, outgoing, incoming) }()
	await := func(kind string) model.ServerMessage {
		t.Helper()
		for {
			select {
			case message := <-incoming:
				if message.Type == kind {
					return message
				}
			case <-ctx.Done():
				t.Fatalf("timed out waiting for %s", kind)
			}
		}
	}
	if await("config").Config.Refresh != 3 {
		t.Fatal("missing config")
	}
	outgoing <- model.ClientMessage{Type: "report", Report: &model.Report{CPU: 25, Sequence: 1}}
	if await("reportAck").Sequence != 1 {
		t.Fatal("invalid live acknowledgement")
	}
	outgoing <- model.ClientMessage{Type: "archive", Archive: &model.Archive{BootID: "boot", Start: 300000, Samples: []model.PackedSample{{0, 0, 1, 25}}}}
	if await("archiveAck").Start != 300000 {
		t.Fatal("invalid archive acknowledgement")
	}
	if err := <-serverErrors; err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream did not stop on cancellation")
	}
}

func TestInvalidTokenReturnsAnAuthenticationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unauthorized", 401) }))
	defer server.Close()
	err := New(server.URL, "revoked").Stream(context.Background(), make(chan model.ClientMessage), make(chan model.ServerMessage, 1))
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected an authentication error: %v", err)
	}
}

// Executed by noise-admin's local Miniflare integration suite when both repositories are present.
func TestCloudflareWorkerProtocolWhenConfigured(t *testing.T) {
	endpoint, token := os.Getenv("NOISE_TEST_WORKER"), os.Getenv("NOISE_TEST_TOKEN")
	if endpoint == "" || token == "" {
		t.Skip("local Worker fixture not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outgoing := make(chan model.ClientMessage, 2)
	incoming := make(chan model.ServerMessage, 8)
	done := make(chan error, 1)
	go func() { done <- New(endpoint, token).Stream(ctx, outgoing, incoming) }()
	await := func(kind string) model.ServerMessage {
		t.Helper()
		for {
			select {
			case message := <-incoming:
				if message.Type == "error" {
					t.Fatal(message.Message)
				}
				if message.Type == kind {
					return message
				}
			case <-ctx.Done():
				t.Fatalf("timed out waiting for %s", kind)
			}
		}
	}
	if cfg := await("config"); cfg.Config.Refresh != 3 || cfg.Config.ArchiveSeconds != 300 {
		t.Fatalf("unexpected worker configuration: %#v", cfg.Config)
	}
	now := time.Now().UnixMilli()
	outgoing <- model.ClientMessage{Type: "report", Report: &model.Report{BootID: "go-agent-smoke", Time: now, Sequence: 0, Uptime: 3600, CPU: 33, Mem: 44, Disk: 55, Load: 0.5, Up: 1024, Down: 2048, TotalUp: 1000, TotalDown: 2000}}
	await("reportAck")
	start := (now - 300000) / 300000 * 300000
	outgoing <- model.ClientMessage{Type: "archive", Archive: &model.Archive{BootID: "go-agent-smoke", Start: start, Samples: []model.PackedSample{{0, 0, 3600, 33, 44, 55, 0.5, 1024, 2048, 1000, 2000}}, Latency: []model.LatencyResult{}}}
	if await("archiveAck").Start != start {
		t.Fatal("worker did not acknowledge the Go archive")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Go worker connection did not stop")
	}
}
