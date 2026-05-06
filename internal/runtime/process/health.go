package process

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// HealthChecker runs MCP `ping` probes against every supervised instance
// at the cadence declared on its spec. Three consecutive failures move
// the instance to crashed; the supervisor's normal restart pipeline
// handles backoff and circuit-breaker.
//
// One HealthChecker per Supervisor; goroutines are managed per instance
// and join cleanly on Stop / instance close.
type HealthChecker struct {
	log *slog.Logger
	sup *Supervisor

	mu      sync.Mutex
	probes  map[string]context.CancelFunc // instance id -> cancel
	stopped bool
}

const (
	healthFailThreshold = 3
)

// NewHealthChecker constructs a HealthChecker bound to the supplied
// supervisor. The supervisor is the source of truth for which instances
// exist; the checker only spins up probe goroutines for instances it has
// been told about via Track.
func NewHealthChecker(log *slog.Logger, sup *Supervisor) *HealthChecker {
	if log == nil {
		log = slog.Default()
	}
	return &HealthChecker{log: log, sup: sup, probes: make(map[string]context.CancelFunc)}
}

// Track starts the probe loop for an instance. The supervisor calls this
// after a successful start. Re-tracking the same id is a no-op so the
// supervisor doesn't have to remember whether it already registered.
func (h *HealthChecker) Track(inst *instance) {
	if h == nil {
		return
	}
	interval := inst.spec.Health.PingInterval.Std()
	if interval <= 0 {
		// Periodic probes disabled.
		return
	}
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return
	}
	if _, exists := h.probes[inst.id]; exists {
		h.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.probes[inst.id] = cancel
	h.mu.Unlock()

	timeout := inst.spec.Health.PingTimeout.Std()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	go h.run(ctx, inst, interval, timeout)
}

// Untrack stops the probe loop for an instance. Idempotent.
func (h *HealthChecker) Untrack(instID string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	cancel, ok := h.probes[instID]
	delete(h.probes, instID)
	h.mu.Unlock()
	if ok {
		cancel()
	}
}

// Stop terminates every probe goroutine. Used on supervisor shutdown.
func (h *HealthChecker) Stop() {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.stopped = true
	cancels := make([]context.CancelFunc, 0, len(h.probes))
	for _, c := range h.probes {
		cancels = append(cancels, c)
	}
	h.probes = make(map[string]context.CancelFunc)
	h.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

func (h *HealthChecker) run(ctx context.Context, inst *instance, interval, timeout time.Duration) {
	failures := 0
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		client, alive := h.snapshot(inst)
		if !alive {
			return
		}
		if client == nil {
			// Instance is between states; skip this cycle.
			failures = 0
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, timeout)
		err := client.Ping(probeCtx)
		cancel()
		if err == nil {
			failures = 0
			continue
		}
		failures++
		h.log.Warn("health probe failed",
			"instance_id", inst.id,
			"server_id", inst.spec.ID,
			"failures", failures,
			"err", err)
		if failures >= healthFailThreshold {
			h.log.Warn("health probe threshold breached; marking crashed",
				"instance_id", inst.id, "server_id", inst.spec.ID)
			// markCrashed runs synchronously from the probe goroutine,
			// which owns its own ctx; the request context that originally
			// spawned the instance is no longer in scope.
			h.sup.markCrashed(inst, err) //nolint:contextcheck
			return
		}
	}
}

// snapshot returns the current client + a liveness flag without holding
// the supervisor lock for long. alive=false means the instance has been
// stopped and the goroutine must exit.
func (h *HealthChecker) snapshot(inst *instance) (clientSnapshot, bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	switch inst.state {
	case StateStopping, StateCrashed, StateBackoff, StateCircuitOpen:
		return nil, false
	}
	return inst.client, true
}

// clientSnapshot is the type the prober calls Ping on. We use a tight
// interface rather than the full southbound.Client to keep this file
// independent of the southbound package's larger surface.
type clientSnapshot interface {
	Ping(ctx context.Context) error
}
