// Package telemetry owns Portico's tracing surface: OpenTelemetry tracer
// init, exporter selection, attribute keys, and W3C trace-context
// propagation across northbound and southbound boundaries.
//
// Subagent fills the implementation; this file owns the public API.
package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

// Config drives Init. The zero value disables tracing entirely (no-op
// tracer, no exporter); operators opt in via portico.yaml.
type Config struct {
	Enabled       bool
	ServiceName   string // default "portico"
	Exporter      string // "otlp_grpc" | "otlp_http" | "stdout" | "none"
	OTLPEndpoint  string // e.g. "localhost:4317"
	OTLPHeaders   map[string]string
	SampleRate    float64 // 0..1; default 1.0
	ResourceAttrs map[string]string
}

// Init wires the global TracerProvider per cfg. Returns a shutdown
// callback the caller must invoke on graceful exit so spans flush.
//
// Subagent fills the body. Implementation must:
//   - register the W3C TraceContext propagator (and Baggage for symmetry).
//   - construct the configured exporter, falling back to a no-op tracer
//     when cfg.Enabled == false (so call sites can unconditionally
//     `tracer.Start(...)` without nil checks).
//   - apply cfg.SampleRate via TraceIDRatioBased.
//   - set service.name + ResourceAttrs on the resource.
//   - return a shutdown that flushes pending spans within ctx.
func Init(ctx context.Context, cfg Config, log *slog.Logger) (Shutdown, error) {
	return initOTel(ctx, cfg, log)
}

// Shutdown flushes pending spans. Idempotent; safe to call from a defer.
type Shutdown func(ctx context.Context) error

// NopShutdown is the default-disabled / no-tracing return.
var NopShutdown Shutdown = func(_ context.Context) error { return nil }

// ErrTracerInitTwice surfaces when Init runs twice; the runtime expects a
// single TracerProvider for the process lifetime.
var ErrTracerInitTwice = errors.New("telemetry: tracer already initialised")

// initOTel is the package-private entry point. Subagent fills.
var (
	initMu   sync.Mutex
	initDone bool
)

// MarkInitialised flips the once-only guard. Subagent calls this from
// initOTel after a successful tracer registration.
func MarkInitialised() error {
	initMu.Lock()
	defer initMu.Unlock()
	if initDone {
		return ErrTracerInitTwice
	}
	initDone = true
	return nil
}

// ResetForTests clears the once-only guard. Tests only.
func ResetForTests() {
	initMu.Lock()
	initDone = false
	initMu.Unlock()
}
