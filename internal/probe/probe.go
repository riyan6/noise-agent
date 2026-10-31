package probe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math"
	"net"
	"sync"
	"time"

	"noise-agent/internal/model"
)

func randomRunID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

const maxQueuedResults = 1000

type worker struct {
	task   model.AgentTask
	cancel context.CancelFunc
}

type Manager struct {
	mu      sync.Mutex
	workers map[string]*worker
	queue   []model.LatencyResult
	notify  chan struct{}
}

func NewManager() *Manager {
	return &Manager{
		workers: make(map[string]*worker),
		notify:  make(chan struct{}, 1),
	}
}

func (m *Manager) Ready() <-chan struct{} {
	return m.notify
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, w := range m.workers {
		w.cancel()
		delete(m.workers, id)
	}
}

func (m *Manager) SyncTasks(parent context.Context, tasks []model.AgentTask) {
	m.mu.Lock()
	defer m.mu.Unlock()

	desired := make(map[string]model.AgentTask, len(tasks))
	for _, t := range tasks {
		if t.ID == "" {
			continue
		}
		if t.Interval < 1 {
			t.Interval = 60
		}
		if t.Timeout < 1 {
			t.Timeout = 5
		}
		desired[t.ID] = t
	}

	for id, existing := range m.workers {
		next, ok := desired[id]
		if !ok || existing.task.Type != next.Type || existing.task.Target != next.Target || existing.task.Interval != next.Interval || existing.task.Timeout != next.Timeout {
			existing.cancel()
			delete(m.workers, id)
		}
	}

	if len(m.queue) > 0 {
		filtered := m.queue[:0]
		for _, r := range m.queue {
			if _, ok := desired[r.TaskID]; ok {
				filtered = append(filtered, r)
			}
		}
		m.queue = filtered
	}

	for id, task := range desired {
		if _, running := m.workers[id]; running {
			continue
		}
		ctx, cancel := context.WithCancel(parent)
		m.workers[id] = &worker{task: task, cancel: cancel}
		go m.runWorker(ctx, task)
	}
}

func (m *Manager) runWorker(ctx context.Context, task model.AgentTask) {
	interval := time.Duration(max(1, task.Interval)) * time.Second
	timeout := time.Duration(max(1, task.Timeout)) * time.Second

	m.executeOnce(ctx, task, timeout)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.executeOnce(ctx, task, timeout)
		}
	}
}

func (m *Manager) executeOnce(ctx context.Context, task model.AgentTask, timeout time.Duration) {
	if ctx.Err() != nil {
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var ms float64
	var err error
	switch task.Type {
	case "tcp":
		ms, err = probeTCP(probeCtx, task.Target, timeout)
	default:
		ms, err = probeICMP(probeCtx, task.Target, timeout)
	}
	if ctx.Err() != nil {
		return
	}

	res := model.LatencyResult{
		TaskID: task.ID,
		RunID:  randomRunID(),
		Time:   time.Now().UnixMilli(),
	}
	maxMs := float64(timeout.Milliseconds())
	if err == nil && !math.IsNaN(ms) && !math.IsInf(ms, 0) && ms >= 0 && ms <= maxMs {
		rounded := math.Round(ms*100) / 100
		res.Status = "ok"
		res.Value = &rounded
	} else if errors.Is(err, errProbeTimeout) || isTimeoutError(err) || ms > maxMs {
		res.Status = "timeout"
		res.Value = nil
	} else {
		res.Status = "error"
		res.Value = nil
	}

	m.enqueue(res)
}

func probeTCP(ctx context.Context, target string, timeout time.Duration) (float64, error) {
	dialer := &net.Dialer{Timeout: timeout}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", target)
	elapsed := time.Since(start).Seconds() * 1000.0
	if err != nil {
		if isTimeoutError(err) {
			return 0, errProbeTimeout
		}
		return 0, err
	}
	_ = conn.Close()
	return elapsed, nil
}

func (m *Manager) enqueue(res model.LatencyResult) {
	m.mu.Lock()
	m.queue = append(m.queue, res)
	if len(m.queue) > maxQueuedResults {
		m.queue = m.queue[len(m.queue)-maxQueuedResults:]
	}
	m.mu.Unlock()

	select {
	case m.notify <- struct{}{}:
	default:
	}
}

func (m *Manager) Drain(limit int) []model.LatencyResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.queue) == 0 {
		return nil
	}
	if limit <= 0 || limit > len(m.queue) {
		limit = len(m.queue)
	}
	batch := make([]model.LatencyResult, limit)
	copy(batch, m.queue[:limit])
	m.queue = m.queue[limit:]
	return batch
}

func (m *Manager) Requeue(batch []model.LatencyResult) {
	if len(batch) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	combined := make([]model.LatencyResult, 0, len(batch)+len(m.queue))
	combined = append(combined, batch...)
	combined = append(combined, m.queue...)
	if len(combined) > maxQueuedResults {
		combined = combined[len(combined)-maxQueuedResults:]
	}
	m.queue = combined
}
