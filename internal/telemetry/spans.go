package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Tracer name shared across every package that opens spans. Centralised
// so a future rename is a single edit.
const tracerName = "github.com/hurtener/Portico_gateway"

// Tracer returns the package-local tracer. Cheap to call repeatedly; the
// SDK caches by name.
func Tracer() trace.Tracer { return otel.Tracer(tracerName) }

// StartSpan opens a span on the active tracer with the given name and
// attribute set. Returns the new ctx + a span; the caller must call
// span.End() when done.
//
// Convenience wrapper so call sites don't import otel/attribute directly
// and so we apply uniform conventions (e.g. always attaching tenant.id
// when present in ctx).
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, trace.WithAttributes(attrs...))
}

// EndOK closes the span with a success status. Useful in defers where
// the caller knows the span recorded an error via RecordErr already.
func EndOK(span trace.Span) {
	if span == nil {
		return
	}
	span.SetStatus(codes.Ok, "")
	span.End()
}

// RecordErr attaches err to the span (when non-nil) and sets the status
// to Error. Returns err unchanged so it can be used in `return RecordErr(span, err)` flows.
func RecordErr(span trace.Span, err error) error {
	if span == nil || err == nil {
		return err
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

// String wraps attribute.String.
func String(key, val string) attribute.KeyValue { return attribute.String(key, val) }

// Bool wraps attribute.Bool.
func Bool(key string, val bool) attribute.KeyValue { return attribute.Bool(key, val) }

// Int wraps attribute.Int.
func Int(key string, val int) attribute.KeyValue { return attribute.Int(key, val) }
