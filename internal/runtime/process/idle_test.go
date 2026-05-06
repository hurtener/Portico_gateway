package process

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestIdleTimer_FiresOnceAfterTimeout(t *testing.T) {
	var fired atomic.Int32
	timer := NewIdleTimer(50*time.Millisecond, func() { fired.Add(1) })
	timer.Start()
	time.Sleep(150 * time.Millisecond)
	if fired.Load() != 1 {
		t.Errorf("fired=%d, want 1", fired.Load())
	}
}

func TestIdleTimer_TickResetsDeadline(t *testing.T) {
	var fired atomic.Int32
	timer := NewIdleTimer(80*time.Millisecond, func() { fired.Add(1) })
	timer.Start()
	for i := 0; i < 4; i++ {
		time.Sleep(40 * time.Millisecond)
		timer.Tick()
	}
	// We've ticked 4 times, each before the 80ms deadline. Should not have fired yet.
	if fired.Load() != 0 {
		t.Errorf("fired prematurely: %d", fired.Load())
	}
	time.Sleep(120 * time.Millisecond)
	if fired.Load() != 1 {
		t.Errorf("expected fire after final wait, got %d", fired.Load())
	}
}

func TestIdleTimer_StopCancels(t *testing.T) {
	var fired atomic.Int32
	timer := NewIdleTimer(50*time.Millisecond, func() { fired.Add(1) })
	timer.Start()
	timer.Stop()
	time.Sleep(120 * time.Millisecond)
	if fired.Load() != 0 {
		t.Errorf("fired after stop: %d", fired.Load())
	}
}

func TestIdleTimer_DisabledIsNoop(t *testing.T) {
	var fired atomic.Int32
	timer := NewIdleTimer(0, func() { fired.Add(1) })
	timer.Start()
	timer.Tick()
	time.Sleep(50 * time.Millisecond)
	if fired.Load() != 0 {
		t.Error("disabled timer should never fire")
	}
}
