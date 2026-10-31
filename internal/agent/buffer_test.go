package agent

import (
	"noise-agent/internal/model"
	"testing"
)

func TestBufferPreservesSamplesUntilMatchingCommitAck(t *testing.T) {
	b := newBuffer("boot")
	start := int64(1_800_000_000_000) / archiveMillis * archiveMillis
	for i := int64(0); i < 100; i++ {
		b.sample(model.Report{BootID: "boot", Time: start + i*3000, Sequence: i, CPU: 12.345, Mem: 30, TotalUp: 100 + i, TotalDown: 200 + i})
	}
	b.latency([]model.LatencyResult{{TaskID: "probe", RunID: "run", Time: start + 30_000, Status: "timeout"}})
	if b.next(start+archiveMillis+5000) != nil {
		t.Fatal("must not archive a window before its probe grace period")
	}
	chunk := b.next(start + archiveMillis + 7000)
	if chunk == nil || len(chunk.Samples) != 100 || len(chunk.Latency) != 1 {
		t.Fatalf("lost raw samples: %#v", chunk)
	}
	if chunk.Samples[1][0] != 3000 || chunk.Samples[1][3] != 12.35 {
		t.Fatal("timestamps or metric precision changed")
	}
	b.ack("another-boot", start)
	if b.next(start+archiveMillis+8000) != chunk {
		t.Fatal("an unrelated acknowledgement removed pending history")
	}
	b.ack("boot", start)
	if b.next(start+archiveMillis+9000) != nil {
		t.Fatal("a committed archive must leave the retry queue")
	}
}

func TestOfflineBufferRemainsBoundedWhileCollectionContinues(t *testing.T) {
	b := newBuffer("boot")
	start := int64(1_800_000_000_000) / archiveMillis * archiveMillis
	for i := int64(0); i < 24*60*20; i++ {
		b.sample(model.Report{BootID: "boot", Time: start + i*3000, Sequence: i})
	}
	if len(b.chunks) > 4 {
		t.Fatalf("offline collection grew without bound: %d windows", len(b.chunks))
	}
	total := 0
	for _, chunk := range b.chunks {
		total += len(chunk.Samples)
	}
	if total > 301 {
		t.Fatalf("too many retained memory samples: %d", total)
	}
}

func TestRefreshNeverSchedulesSubThreeSecondReports(t *testing.T) {
	for _, value := range []int{-1, 0, 1, 2} {
		if clampRefresh(value) != 3 {
			t.Fatal("unsafe reporting interval")
		}
	}
	if clampRefresh(3) != 3 || clampRefresh(5) != 5 || clampRefresh(60) != 10 {
		t.Fatal("refresh clamp is inconsistent")
	}
}
