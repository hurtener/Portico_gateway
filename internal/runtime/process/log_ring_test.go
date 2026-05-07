package process

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLogRing_PublishAndSnapshot(t *testing.T) {
	r := NewLogRing(1024)
	r.Publish(LogLine{Text: "first\n", Stream: string(LogStreamStdout)})
	r.Publish(LogLine{Text: "second\n", Stream: string(LogStreamStderr)})

	snap := r.Snapshot(time.Time{})
	if len(snap) != 2 {
		t.Fatalf("snapshot got %d, want 2", len(snap))
	}
	if snap[0].Text != "first\n" || snap[1].Text != "second\n" {
		t.Fatalf("ordering wrong: %+v", snap)
	}
}

func TestLogRing_DropsOnByteCap(t *testing.T) {
	// Each line is ~16 bytes; cap to 32 so older lines drop.
	r := NewLogRing(32)
	for i := 0; i < 5; i++ {
		r.Publish(LogLine{Text: "0123456789ABCDEF"})
	}
	snap := r.Snapshot(time.Time{})
	if len(snap) > 3 {
		t.Fatalf("snapshot %d entries; ring should have dropped older ones (cap=32, line=16)", len(snap))
	}
}

func TestLogRing_SubscribeReplaysHistory(t *testing.T) {
	r := NewLogRing(4096)
	r.Publish(LogLine{Text: "A\n", Stream: string(LogStreamStdout)})
	r.Publish(LogLine{Text: "B\n", Stream: string(LogStreamStdout)})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	ch := r.Subscribe(ctx, time.Time{})

	var got []string
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
loop:
	for {
		select {
		case l, ok := <-ch:
			if !ok {
				break loop
			}
			got = append(got, l.Text)
		case <-timer.C:
			break loop
		}
	}
	if len(got) < 2 || !contains(got, "A\n") || !contains(got, "B\n") {
		t.Fatalf("expected A and B replay, got %v", got)
	}
}

func TestLogRing_SubscribeFutureLines(t *testing.T) {
	r := NewLogRing(4096)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := r.Subscribe(ctx, time.Now().UTC())

	go func() {
		time.Sleep(10 * time.Millisecond)
		r.Publish(LogLine{Text: "live\n"})
	}()

	select {
	case l := <-ch:
		if l.Text != "live\n" {
			t.Fatalf("unexpected %q", l.Text)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timed out waiting for future line")
	}
}

func TestLogRing_SubscribeSinceFilters(t *testing.T) {
	r := NewLogRing(4096)
	old := time.Now().UTC().Add(-time.Hour)
	r.Publish(LogLine{At: old, Text: "old\n"})
	r.Publish(LogLine{At: time.Now().UTC(), Text: "recent\n"})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	ch := r.Subscribe(ctx, time.Now().UTC().Add(-time.Minute))

	got := drain(ch, 100*time.Millisecond)
	if contains(got, "old\n") {
		t.Fatalf("'old' should have been filtered: %v", got)
	}
	if !contains(got, "recent\n") {
		t.Fatalf("'recent' missing from %v", got)
	}
}

func TestLogRing_CloseClosesSubscribers(t *testing.T) {
	r := NewLogRing(1024)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := r.Subscribe(ctx, time.Time{})
	r.Close()

	// Channel should close.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatalf("expected channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatalf("subscriber channel did not close on ring close")
	}
}

func TestPumpReaderToRing(t *testing.T) {
	r := NewLogRing(1024)
	reader := strings.NewReader("alpha\nbeta\n")
	PumpReaderToRing(reader, r, LogStreamStdout)
	snap := r.Snapshot(time.Time{})
	if len(snap) != 2 {
		t.Fatalf("got %d lines, want 2", len(snap))
	}
}

func TestLogRing_PublishLog_AppendsNewline(t *testing.T) {
	r := NewLogRing(1024)
	r.PublishLog("stdout", "no newline")
	snap := r.Snapshot(time.Time{})
	if len(snap) != 1 || snap[0].Text != "no newline\n" {
		t.Fatalf("expected newline append, got %+v", snap)
	}
}

func TestLogRing_PublishLog_PreservesNewline(t *testing.T) {
	r := NewLogRing(1024)
	r.PublishLog("stderr", "with newline\n")
	snap := r.Snapshot(time.Time{})
	if len(snap) != 1 || snap[0].Text != "with newline\n" {
		t.Fatalf("expected single newline, got %+v", snap)
	}
}

func TestLogRingRegistry_LogsFor_Subscribes(t *testing.T) {
	g := NewLogRingRegistry()
	r := g.Acquire("t", "s")
	r.Publish(LogLine{Text: "hi\n"})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	ch := g.LogsFor(ctx, "t", "s", time.Time{})
	got := drain(ch, 100*time.Millisecond)
	if !contains(got, "hi\n") {
		t.Fatalf("expected hi from LogsFor, got %v", got)
	}
}

func TestLogRingRegistry_LogsFor_Nil(t *testing.T) {
	var g *LogRingRegistry
	ch := g.LogsFor(context.Background(), "t", "s", time.Time{})
	if _, ok := <-ch; ok {
		t.Fatalf("expected closed channel from nil registry")
	}
}

func TestLogRingRegistry_PerInstance(t *testing.T) {
	g := NewLogRingRegistry()
	r := g.Acquire("tenant-a", "server-1")
	r2 := g.Acquire("tenant-a", "server-1")
	if r != r2 {
		t.Fatalf("expected same ring for same key")
	}
	rb := g.Acquire("tenant-b", "server-1")
	if rb == r {
		t.Fatalf("rings for different tenants should differ")
	}
	if g.Get("tenant-a", "server-1") == nil {
		t.Fatalf("registry lost the ring")
	}
	g.Drop("tenant-a", "server-1")
	if g.Get("tenant-a", "server-1") != nil {
		t.Fatalf("ring not dropped")
	}
	g.CloseAll()
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func drain(ch <-chan LogLine, until time.Duration) []string {
	var out []string
	timer := time.NewTimer(until)
	defer timer.Stop()
	for {
		select {
		case l, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, l.Text)
		case <-timer.C:
			return out
		}
	}
}
