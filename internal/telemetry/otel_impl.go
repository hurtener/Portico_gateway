package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/hurtener/Portico_gateway/internal/telemetry/spanstore"
)

// defaultServiceName is used when cfg.ServiceName is empty.
const defaultServiceName = "portico"

// initOTel is the package-private implementation backed by the OTel SDK.
//
// Behaviour summary:
//   - When tracing is disabled (Enabled=false, Exporter empty, or "none"),
//     register only the W3C trace-context + Baggage propagator so inbound
//     traceparent headers/_meta entries continue to round-trip even though
//     no spans are recorded. No TracerProvider is registered, no init flag
//     is flipped, NopShutdown is returned.
//   - Otherwise build the chosen exporter (otlp_grpc, otlp_http, stdout),
//     wrap it in a batched TracerProvider with a TraceIDRatioBased sampler
//     and a resource carrying service.name + cfg.ResourceAttrs, and set
//     the global TracerProvider + propagator. MarkInitialised guards
//     against double init.
func initOTel(ctx context.Context, cfg Config, log *slog.Logger) (Shutdown, error) {
	// Always register the W3C propagator so transports can extract/inject
	// even when tracing is disabled. This is cheap and idempotent.
	setPropagator()

	if !cfg.Enabled || cfg.Exporter == "" || cfg.Exporter == "none" {
		if log != nil {
			log.Debug("telemetry: tracing disabled; propagator-only mode",
				"enabled", cfg.Enabled,
				"exporter", cfg.Exporter,
			)
		}
		return NopShutdown, nil
	}

	exporter, err := buildExporter(ctx, cfg)
	if err != nil {
		return NopShutdown, err
	}

	// Phase 11: tee the exporter into the local span store when
	// configured, so the session inspector can render a full waterfall
	// without relying on the external collector.
	if cfg.SpanStore != nil {
		// Tee starts a detached drain goroutine; the worker context is
		// intentionally background (it outlives any request). See
		// spanstore.exporter_hook.go for the rationale.
		exporter = spanstore.Tee(exporter, cfg.SpanStore, spanstore.HookOptions{Logger: log}) //nolint:contextcheck // detached drain worker
	}

	res, err := buildResource(cfg)
	if err != nil {
		// Best-effort: shut the exporter we already built so we don't leak.
		_ = exporter.Shutdown(ctx)
		return NopShutdown, fmt.Errorf("telemetry: build resource: %w", err)
	}

	sampleRate := cfg.SampleRate
	if sampleRate == 0 {
		sampleRate = 1.0
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(sampleRate)),
	)

	if err := MarkInitialised(); err != nil {
		// Already initialised — tear down what we just built so we don't
		// leak the batcher goroutine, then surface the sentinel. The
		// caller's ctx may already be done, so derive a fresh one with a
		// short bound for the shutdown.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second) //nolint:contextcheck
		defer cancel()
		_ = tp.Shutdown(shutdownCtx) //nolint:contextcheck
		return NopShutdown, err
	}

	otel.SetTracerProvider(tp)

	if log != nil {
		log.Info("telemetry: tracer initialised",
			"exporter", cfg.Exporter,
			"service", serviceName(cfg),
			"sample_rate", sampleRate,
		)
	}

	//nolint:contextcheck // callerCtx may be a fresh shutdown ctx; bound below.
	shutdown := func(callerCtx context.Context) error {
		// If the caller's ctx is already done, derive a fresh bounded
		// context so the batcher gets a chance to flush rather than
		// returning ctx.Err() immediately.
		if callerCtx == nil || callerCtx.Err() != nil {
			var cancel context.CancelFunc
			callerCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second) //nolint:contextcheck
			defer cancel()
		}
		return tp.Shutdown(callerCtx)
	}
	return shutdown, nil
}

// setPropagator registers the W3C TraceContext + Baggage composite
// propagator as the global text-map propagator.
func setPropagator() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

// buildExporter constructs the configured span exporter.
func buildExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	switch cfg.Exporter {
	case "otlp_grpc":
		opts := []otlptracegrpc.Option{}
		if cfg.OTLPEndpoint != "" {
			opts = append(opts, otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint))
		}
		if len(cfg.OTLPHeaders) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(cfg.OTLPHeaders))
		}
		// Default to insecure unless headers carry an Authorization-style
		// token (heuristic: presence of "authorization" or "api-key").
		if !headersImplyAuth(cfg.OTLPHeaders) {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exp, err := otlptracegrpc.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("telemetry: otlp_grpc exporter: %w", err)
		}
		return exp, nil
	case "otlp_http":
		opts := []otlptracehttp.Option{}
		if cfg.OTLPEndpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpoint(cfg.OTLPEndpoint))
		}
		if len(cfg.OTLPHeaders) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(cfg.OTLPHeaders))
		}
		if !headersImplyAuth(cfg.OTLPHeaders) {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		exp, err := otlptracehttp.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("telemetry: otlp_http exporter: %w", err)
		}
		return exp, nil
	case "stdout":
		exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("telemetry: stdout exporter: %w", err)
		}
		return exp, nil
	default:
		return nil, fmt.Errorf("telemetry: unknown exporter %q (want otlp_grpc | otlp_http | stdout | none)", cfg.Exporter)
	}
}

// headersImplyAuth returns true when the OTLP header map carries an entry
// that looks like an authentication credential. Used to flip off
// WithInsecure so we don't ship bearer tokens over plaintext.
func headersImplyAuth(headers map[string]string) bool {
	for k := range headers {
		switch normaliseHeader(k) {
		case "authorization", "api-key", "x-api-key":
			return true
		}
	}
	return false
}

// normaliseHeader lowercases an HTTP header name without pulling in
// strings just for ToLower at module scope (and avoids allocations on
// the hot path the caller is unlikely to hit).
func normaliseHeader(name string) string {
	out := make([]byte, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

// buildResource assembles the OTel Resource for the TracerProvider.
func buildResource(cfg Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(serviceName(cfg)),
	}
	for k, v := range cfg.ResourceAttrs {
		attrs = append(attrs, attribute.String(k, v))
	}

	custom := resource.NewWithAttributes(semconv.SchemaURL, attrs...)

	// Merge with the SDK's default resource (process info, host, etc.)
	// so the exported span data is well-rooted; on schema-URL conflict
	// fall back to the explicit attrs so a future SDK upgrade doesn't
	// break Init.
	merged, err := resource.Merge(resource.Default(), custom)
	if err != nil {
		if errors.Is(err, resource.ErrSchemaURLConflict) {
			return custom, nil
		}
		return nil, err
	}
	return merged, nil
}

// serviceName returns cfg.ServiceName or the package default.
func serviceName(cfg Config) string {
	if cfg.ServiceName == "" {
		return defaultServiceName
	}
	return cfg.ServiceName
}
