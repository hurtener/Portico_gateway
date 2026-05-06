package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// newTestLogger returns a slog.Logger that discards output so tests stay
// quiet but the Init code path still receives a non-nil logger.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// shutdownWithTimeout invokes a Shutdown with a bounded context so a
// hung exporter can never wedge `go test`.
func shutdownWithTimeout(t *testing.T, sd Shutdown) {
	t.Helper()
	if sd == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sd(ctx); err != nil {
		t.Errorf("shutdown returned error: %v", err)
	}
}

// resetGlobals returns the OTel globals to a clean state between
// subtests so they don't observe each other's tracer providers.
func resetGlobals(t *testing.T) {
	t.Helper()
	ResetForTests()
	// Restore the propagator-only mode so subsequent tests see a known
	// baseline. The next Init call (if any) will overwrite this.
	setPropagator()
}

func TestInit_DisabledReturnsNopShutdown(t *testing.T) {
	resetGlobals(t)
	defer resetGlobals(t)

	cfg := Config{Enabled: false}
	sd, err := Init(context.Background(), cfg, newTestLogger())
	if err != nil {
		t.Fatalf("Init(disabled) error: %v", err)
	}
	if reflect.ValueOf(sd).Pointer() != reflect.ValueOf(NopShutdown).Pointer() {
		t.Fatalf("expected NopShutdown when disabled, got non-nop function")
	}

	// A second call must also succeed (no init-flag flip when disabled).
	sd2, err := Init(context.Background(), cfg, newTestLogger())
	if err != nil {
		t.Fatalf("Init(disabled) second call: %v", err)
	}
	if reflect.ValueOf(sd2).Pointer() != reflect.ValueOf(NopShutdown).Pointer() {
		t.Fatalf("expected NopShutdown on second disabled call")
	}

	// Even in disabled mode, the W3C propagator must be registered so
	// inbound traceparent headers continue to round-trip.
	if otel.GetTextMapPropagator() == nil {
		t.Fatal("expected propagator to be registered even when tracing disabled")
	}
}

func TestInit_NoneExporterReturnsNopShutdown(t *testing.T) {
	resetGlobals(t)
	defer resetGlobals(t)

	cfg := Config{Enabled: true, Exporter: "none"}
	sd, err := Init(context.Background(), cfg, newTestLogger())
	if err != nil {
		t.Fatalf("Init(none) error: %v", err)
	}
	if reflect.ValueOf(sd).Pointer() != reflect.ValueOf(NopShutdown).Pointer() {
		t.Fatalf("expected NopShutdown when exporter=none")
	}
}

func TestInit_StdoutExporter_RegistersTracerProvider(t *testing.T) {
	resetGlobals(t)
	defer resetGlobals(t)

	cfg := Config{
		Enabled:     true,
		ServiceName: "portico-test",
		Exporter:    "stdout",
		SampleRate:  1.0,
	}
	sd, err := Init(context.Background(), cfg, newTestLogger())
	if err != nil {
		t.Fatalf("Init(stdout) error: %v", err)
	}
	defer shutdownWithTimeout(t, sd)

	tp, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	if !ok {
		t.Fatalf("expected *sdktrace.TracerProvider after Init, got %T", otel.GetTracerProvider())
	}
	if tp == nil {
		t.Fatal("registered tracer provider is nil")
	}

	// Sanity-check that we can actually start a span with the registered
	// provider; if Init wired the SDK incorrectly this would panic or
	// produce a never-recording span.
	tracer := otel.Tracer("portico/test")
	_, span := tracer.Start(context.Background(), "smoke")
	if !span.SpanContext().IsValid() {
		t.Fatal("expected valid span context from registered SDK provider")
	}
	span.End()
}

func TestInit_TwiceReturnsError(t *testing.T) {
	resetGlobals(t)
	defer resetGlobals(t)

	cfg := Config{Enabled: true, Exporter: "stdout", SampleRate: 1.0}

	sd, err := Init(context.Background(), cfg, newTestLogger())
	if err != nil {
		t.Fatalf("first Init: %v", err)
	}
	defer shutdownWithTimeout(t, sd)

	_, err = Init(context.Background(), cfg, newTestLogger())
	if err == nil {
		t.Fatal("expected ErrTracerInitTwice on second Init, got nil")
	}
	if !errors.Is(err, ErrTracerInitTwice) {
		t.Fatalf("expected ErrTracerInitTwice, got %v", err)
	}
}

func TestInit_UnknownExporter_ReturnsError(t *testing.T) {
	resetGlobals(t)
	defer resetGlobals(t)

	cfg := Config{Enabled: true, Exporter: "kafka"}
	_, err := Init(context.Background(), cfg, newTestLogger())
	if err == nil {
		t.Fatal("expected error for unknown exporter")
	}
}

func TestInit_DefaultsSampleRate(t *testing.T) {
	resetGlobals(t)
	defer resetGlobals(t)

	cfg := Config{Enabled: true, Exporter: "stdout", SampleRate: 0}
	sd, err := Init(context.Background(), cfg, newTestLogger())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer shutdownWithTimeout(t, sd)

	// We can't introspect the sampler directly through the public API,
	// but we can confirm a span is recorded — at sample-rate 1.0 every
	// trace is sampled, while at the would-be default of 0 none are.
	tracer := otel.Tracer("portico/test")
	_, span := tracer.Start(context.Background(), "default-rate")
	if !span.IsRecording() {
		t.Fatal("expected default sample rate to record spans")
	}
	span.End()
}

func TestPropagation_RoundTrip(t *testing.T) {
	resetGlobals(t)
	defer resetGlobals(t)

	cfg := Config{Enabled: true, Exporter: "stdout", SampleRate: 1.0}
	sd, err := Init(context.Background(), cfg, newTestLogger())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer shutdownWithTimeout(t, sd)

	tracer := otel.Tracer("portico/test")
	ctx, span := tracer.Start(context.Background(), "roundtrip")
	defer span.End()

	srcSC := span.SpanContext()
	if !srcSC.IsValid() {
		t.Fatal("source span context invalid")
	}

	// Inject onto an http.Header.
	h := http.Header{}
	InjectIntoHTTP(ctx, h)
	if h.Get(HeaderTraceparent) == "" {
		t.Fatalf("expected %s header to be set", HeaderTraceparent)
	}

	// Extract on a fresh context — the resulting span context must
	// carry the same trace ID.
	extractCtx := ExtractFromHTTP(context.Background(), h)
	_, dstSpan := tracer.Start(extractCtx, "downstream")
	defer dstSpan.End()

	if got, want := dstSpan.SpanContext().TraceID(), srcSC.TraceID(); got != want {
		t.Fatalf("trace id did not propagate: got %s, want %s", got, want)
	}
}

func TestInjectIntoMCPMeta_RoundTrip(t *testing.T) {
	resetGlobals(t)
	defer resetGlobals(t)

	cfg := Config{Enabled: true, Exporter: "stdout", SampleRate: 1.0}
	sd, err := Init(context.Background(), cfg, newTestLogger())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer shutdownWithTimeout(t, sd)

	tracer := otel.Tracer("portico/test")
	ctx, span := tracer.Start(context.Background(), "meta-roundtrip")
	defer span.End()

	srcSC := span.SpanContext()

	meta := InjectIntoMCPMeta(ctx, nil)
	tp, ok := meta[MetaTraceparentKey].(string)
	if !ok || tp == "" {
		t.Fatalf("expected traceparent in meta, got %v", meta)
	}

	// Round-trip via JSON to mirror how the dispatcher actually carries
	// _meta blocks (json.RawMessage on inbound MCP requests).
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}

	extractCtx := ExtractFromMCPMeta(context.Background(), raw)
	_, dstSpan := tracer.Start(extractCtx, "downstream-meta")
	defer dstSpan.End()

	if got, want := dstSpan.SpanContext().TraceID(), srcSC.TraceID(); got != want {
		t.Fatalf("trace id did not propagate via _meta: got %s, want %s", got, want)
	}
}

func TestExtractFromMCPMeta_EmptyMeta(t *testing.T) {
	resetGlobals(t)
	defer resetGlobals(t)

	// Even without Init, the propagator-only path should handle empty
	// metadata as a no-op (return ctx unchanged).
	in := context.Background()
	out := ExtractFromMCPMeta(in, nil)
	if out != in {
		t.Fatal("expected ExtractFromMCPMeta(nil) to return ctx unchanged")
	}

	out = ExtractFromMCPMeta(in, json.RawMessage(`{}`))
	if out != in {
		t.Fatal("expected ExtractFromMCPMeta on traceparent-less meta to return ctx unchanged")
	}
}

func TestTraceparentFor_NoSpan_ReturnsEmpty(t *testing.T) {
	resetGlobals(t)
	defer resetGlobals(t)

	// Set the propagator so TraceparentFor isn't short-circuited by a
	// missing global; with no active span the carrier must still be empty.
	setPropagator()

	if got := TraceparentFor(context.Background()); got != "" {
		t.Fatalf("expected empty traceparent without active span, got %q", got)
	}
}
