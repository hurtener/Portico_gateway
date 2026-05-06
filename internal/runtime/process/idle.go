package process

import (
	"sync"
	"time"
)

// IdleTimer fires onIdle once when no Tick arrives within timeout. Tick
// resets the deadline. Calling Stop cancels any pending firing.
type IdleTimer struct {
	timeout time.Duration
	onIdle  func()

	mu      sync.Mutex
	timer   *time.Timer
	stopped bool
	fired   bool
}

// NewIdleTimer constructs a timer. timeout==0 disables idle handling and
// Tick / Start are no-ops.
func NewIdleTimer(timeout time.Duration, onIdle func()) *IdleTimer {
	return &IdleTimer{timeout: timeout, onIdle: onIdle}
}

// Start arms the timer. Safe to call multiple times; first call wins.
func (t *IdleTimer) Start() {
	if t == nil || t.timeout <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timer != nil || t.stopped {
		return
	}
	t.timer = time.AfterFunc(t.timeout, t.fire)
}

// Tick resets the deadline. No-op when disabled or already stopped.
func (t *IdleTimer) Tick() {
	if t == nil || t.timeout <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped || t.fired {
		return
	}
	if t.timer == nil {
		t.timer = time.AfterFunc(t.timeout, t.fire)
		return
	}
	t.timer.Reset(t.timeout)
}

// Stop cancels the pending fire. Idempotent.
func (t *IdleTimer) Stop() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
}

func (t *IdleTimer) fire() {
	t.mu.Lock()
	if t.stopped || t.fired {
		t.mu.Unlock()
		return
	}
	t.fired = true
	cb := t.onIdle
	t.mu.Unlock()
	if cb != nil {
		cb()
	}
}
