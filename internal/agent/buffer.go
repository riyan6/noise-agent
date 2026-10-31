package agent

import (
	"math"
	"noise-agent/internal/model"
	"sort"
)

const archiveMillis int64 = 300_000
const retryMillis int64 = 600_000

type buffer struct {
	chunks map[int64]*model.Archive
	bootID string
}

func newBuffer(bootID string) *buffer {
	return &buffer{chunks: map[int64]*model.Archive{}, bootID: bootID}
}
func (b *buffer) chunk(at int64) *model.Archive {
	start := at / archiveMillis * archiveMillis
	if b.chunks[start] == nil {
		b.chunks[start] = &model.Archive{BootID: b.bootID, Start: start, Samples: []model.PackedSample{}, Latency: []model.LatencyResult{}}
	}
	return b.chunks[start]
}
func (b *buffer) sample(r model.Report) {
	b.prune(r.Time)
	chunk := b.chunk(r.Time)
	round := func(n float64) float64 { return math.Round(n*100) / 100 }
	chunk.Samples = append(chunk.Samples, model.PackedSample{float64(r.Time - chunk.Start), float64(r.Sequence), round(r.Uptime), round(r.CPU), round(r.Mem), round(r.Disk), round(r.Load), round(r.Up), round(r.Down), float64(r.TotalUp), float64(r.TotalDown)})
}
func (b *buffer) latency(results []model.LatencyResult) {
	for _, result := range results {
		chunk := b.chunk(result.Time)
		if len(chunk.Latency) < 160 {
			chunk.Latency = append(chunk.Latency, result)
		}
	}
}
func (b *buffer) next(now int64) *model.Archive {
	b.prune(now)
	starts := make([]int64, 0, len(b.chunks))
	for start := range b.chunks {
		if start+archiveMillis+6000 < now {
			starts = append(starts, start)
		}
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })
	if len(starts) == 0 {
		return nil
	}
	return b.chunks[starts[0]]
}
func (b *buffer) ack(bootID string, start int64) {
	if bootID == b.bootID {
		delete(b.chunks, start)
	}
}
func (b *buffer) prune(now int64) {
	for start := range b.chunks {
		if start+archiveMillis < now-retryMillis {
			delete(b.chunks, start)
		}
	}
}
