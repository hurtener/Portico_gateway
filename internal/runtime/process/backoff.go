// Package process owns the per-instance lifecycle of southbound stdio
// servers: spawn, restart on failure, idle-timeout, log capture, and
// resource limits. Phase 2 wires this in and exposes the supervisor
// state through the registry.
//
// Subdirectories live in this same package — the lifecycle is small
// enough that a single package keeps the moving parts visible.
package process

import (
	"math/rand"
	"time"
)

// Backoff computes exponential delays with jitter. Stateless — the caller
// passes the attempt number.
type Backoff struct {
	Initial    time.Duration
	Max        time.Duration
	Multiplier float64
	Jitter     float64 // 0..1 — fraction of the computed value
}

// DefaultBackoff returns the policy from the Phase 2 plan: 500ms initial,
// 30s max, 2.0 multiplier, 0.2 jitter.
func DefaultBackoff() Backoff {
	return Backoff{
		Initial:    500 * time.Millisecond,
		Max:        30 * time.Second,
		Multiplier: 2.0,
		Jitter:     0.2,
	}
}

// Next returns the delay before attempt n (1-indexed). attempt=1 returns
// approximately Initial, attempt=N returns
// min(Max, Initial * Multiplier^(N-1)) ± Jitter.
func (b Backoff) Next(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := float64(b.Initial)
	for i := 1; i < attempt; i++ {
		d *= b.Multiplier
		if d > float64(b.Max) {
			d = float64(b.Max)
			break
		}
	}
	if b.Jitter > 0 {
		// Symmetric jitter: ±Jitter fraction.
		jitter := (rand.Float64()*2 - 1) * b.Jitter * d //nolint:gosec // backoff jitter, not security-sensitive
		d += jitter
	}
	if d < 0 {
		d = 0
	}
	if d > float64(b.Max) {
		d = float64(b.Max)
	}
	return time.Duration(d)
}
