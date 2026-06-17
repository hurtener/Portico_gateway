// Package quota enforces per-tenant LLM rate and usage limits with in-memory
// rolling windows. It is dependency-light (no storage import): callers pass the
// tenant's resolved Limits and the engine's token usage.
//
// Enforcement is best-effort and per-process: a single Portico instance tracks
// its own windows. Phase 19 (scale-out) replaces this with a shared counter.
package quota

import (
	"sync"
	"time"
)

// Limits is a tenant's LLM quota, decoupled from the storage type. Zero on any
// field means "no limit" for that dimension.
type Limits struct {
	RequestsPerMinute int
	TokensPerMinute   int
	TokensPerDay      int
}

// ExceededError reports which limit a request would exceed.
type ExceededError struct{ Limit string }

func (e *ExceededError) Error() string { return "llm quota exceeded: " + e.Limit }

type usage struct {
	at     time.Time
	tokens int
}

type window struct {
	requests []time.Time // request timestamps (pruned to the last minute)
	tokens   []usage     // token usage entries (pruned to the last day)
}

// Enforcer tracks per-tenant rolling windows. Safe for concurrent use.
type Enforcer struct {
	mu      sync.Mutex
	windows map[string]*window
	now     func() time.Time // injectable for tests
}

// NewEnforcer returns an Enforcer using the wall clock.
func NewEnforcer() *Enforcer {
	return &Enforcer{windows: make(map[string]*window), now: time.Now}
}

// Check verifies the tenant is under its limits and, if so, records the request
// against the per-minute request window. Call BEFORE dispatch. Returns an
// *ExceededError naming the breached limit when over.
func (e *Enforcer) Check(tenantID string, lim Limits) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	w := e.windowLocked(tenantID)
	w.prune(now)

	if lim.RequestsPerMinute > 0 && len(w.requests) >= lim.RequestsPerMinute {
		return &ExceededError{Limit: "requests_per_minute"}
	}
	minuteTokens, dayTokens := w.sums(now)
	if lim.TokensPerMinute > 0 && minuteTokens >= lim.TokensPerMinute {
		return &ExceededError{Limit: "tokens_per_minute"}
	}
	if lim.TokensPerDay > 0 && dayTokens >= lim.TokensPerDay {
		return &ExceededError{Limit: "tokens_per_day"}
	}

	w.requests = append(w.requests, now)
	return nil
}

// RecordUsage records token usage for a tenant after a dispatch completes.
func (e *Enforcer) RecordUsage(tenantID string, tokens int) {
	if tokens <= 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	w := e.windowLocked(tenantID)
	w.tokens = append(w.tokens, usage{at: e.now(), tokens: tokens})
}

func (e *Enforcer) windowLocked(tenantID string) *window {
	w := e.windows[tenantID]
	if w == nil {
		w = &window{}
		e.windows[tenantID] = w
	}
	return w
}

func (w *window) prune(now time.Time) {
	minuteAgo := now.Add(-time.Minute)
	dayAgo := now.Add(-24 * time.Hour)
	keptReq := w.requests[:0]
	for _, t := range w.requests {
		if t.After(minuteAgo) {
			keptReq = append(keptReq, t)
		}
	}
	w.requests = keptReq
	keptTok := w.tokens[:0]
	for _, u := range w.tokens {
		if u.at.After(dayAgo) {
			keptTok = append(keptTok, u)
		}
	}
	w.tokens = keptTok
}

func (w *window) sums(now time.Time) (minute, day int) {
	minuteAgo := now.Add(-time.Minute)
	for _, u := range w.tokens {
		day += u.tokens
		if u.at.After(minuteAgo) {
			minute += u.tokens
		}
	}
	return minute, day
}
