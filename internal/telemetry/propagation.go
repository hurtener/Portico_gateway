package telemetry

import (
	"context"
	"encoding/json"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// W3C trace-context header names. Mirrored here so transports don't
// hardcode strings.
const (
	HeaderTraceparent = "traceparent"
	HeaderTracestate  = "tracestate"
)

// MetaTraceparentKey is the JSON key the dispatcher injects into MCP
// `_meta` blocks for inter-server trace propagation.
const MetaTraceparentKey = "traceparent"

// ExtractFromHTTP returns ctx augmented with any trace context the
// incoming request carries. Falls through unchanged when the request has
// no traceparent header.
func ExtractFromHTTP(ctx context.Context, h http.Header) context.Context {
	prop := otel.GetTextMapPropagator()
	if prop == nil {
		return ctx
	}
	return prop.Extract(ctx, propagation.HeaderCarrier(h))
}

// InjectIntoHTTP writes the active trace context onto h.
func InjectIntoHTTP(ctx context.Context, h http.Header) {
	prop := otel.GetTextMapPropagator()
	if prop == nil {
		return
	}
	prop.Inject(ctx, propagation.HeaderCarrier(h))
}

// ExtractFromMCPMeta reads `_meta.traceparent` (and tracestate) from a
// JSON blob and returns ctx augmented with it. Used on inbound paths
// when a client supplies meta but no HTTP-level traceparent (stdio).
func ExtractFromMCPMeta(ctx context.Context, meta json.RawMessage) context.Context {
	if len(meta) == 0 {
		return ctx
	}
	var m map[string]any
	if err := json.Unmarshal(meta, &m); err != nil {
		return ctx
	}
	tp, _ := m[MetaTraceparentKey].(string)
	ts, _ := m["tracestate"].(string)
	if tp == "" {
		return ctx
	}
	carrier := propagation.MapCarrier{HeaderTraceparent: tp}
	if ts != "" {
		carrier[HeaderTracestate] = ts
	}
	prop := otel.GetTextMapPropagator()
	if prop == nil {
		return ctx
	}
	return prop.Extract(ctx, carrier)
}

// InjectIntoMCPMeta writes the active trace context into the supplied
// meta map (creating the entries if missing). Returns the same map for
// chaining.
func InjectIntoMCPMeta(ctx context.Context, meta map[string]any) map[string]any {
	if meta == nil {
		meta = map[string]any{}
	}
	carrier := propagation.MapCarrier{}
	prop := otel.GetTextMapPropagator()
	if prop == nil {
		return meta
	}
	prop.Inject(ctx, carrier)
	if v := carrier[HeaderTraceparent]; v != "" {
		meta[MetaTraceparentKey] = v
	}
	if v := carrier[HeaderTracestate]; v != "" {
		meta["tracestate"] = v
	}
	return meta
}

// TraceparentFor returns the current active traceparent string (if any).
// Used by stdio spawn so the supervisor can inject MCP_TRACEPARENT in the
// child env.
func TraceparentFor(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	prop := otel.GetTextMapPropagator()
	if prop == nil {
		return ""
	}
	prop.Inject(ctx, carrier)
	return carrier[HeaderTraceparent]
}
