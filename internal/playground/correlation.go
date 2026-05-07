package playground

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
)

// AuditQuerier is the seam the correlation aggregator depends on. The
// real implementation is *audit.Store.
type AuditQuerier interface {
	Query(ctx context.Context, q audit.Query) ([]audit.Event, string, error)
}

// CorrelationFilter narrows the bundle to a single playground session +
// optional incremental cursor.
type CorrelationFilter struct {
	SessionID string
	Since     time.Time
}

// SpanNode is the span-tree shape the playground frontend renders. A
// Phase 11 follow-up will extend it with attributes & links; for now we
// surface a "trace lite" view derived from `tool_call.*` audit events.
type SpanNode struct {
	SpanID     string            `json:"span_id"`
	ParentID   string            `json:"parent_id,omitempty"`
	Name       string            `json:"name"`
	StartedAt  time.Time         `json:"started_at"`
	EndedAt    time.Time         `json:"ended_at,omitempty"`
	Status     string            `json:"status"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// PolicyDecisionLite is the narrow shape the playground surfaces for
// the Policy tab. The full policy.DryRunResult is a heavier shape that
// the Phase 9 /policy/dry-run page renders; the playground re-uses the
// same evaluator output but shipped through the audit channel.
type PolicyDecisionLite struct {
	Tool     string         `json:"tool,omitempty"`
	Decision string         `json:"decision"`
	Reason   string         `json:"reason,omitempty"`
	Detail   map[string]any `json:"detail,omitempty"`
	At       time.Time      `json:"at"`
}

// Bundle is the canonical correlation payload.
type Bundle struct {
	SessionID    string               `json:"session_id"`
	Spans        []SpanNode           `json:"spans"`
	Audits       []audit.Event        `json:"audits"`
	Policy       []PolicyDecisionLite `json:"policy"`
	Drift        []audit.Event        `json:"drift"`
	LastEventAge string               `json:"last_event_age,omitempty"` // human-readable since last event
	GeneratedAt  time.Time            `json:"generated_at"`
}

// Correlator collates spans, audits, policy decisions, and drift events
// for one playground session. It pulls every input from a single audit
// query (the audit log is the canonical source for everything other
// than spans, which the playground synthesises from tool_call.* events).
type Correlator struct {
	audit AuditQuerier
}

// NewCorrelator constructs a Correlator.
func NewCorrelator(a AuditQuerier) *Correlator {
	return &Correlator{audit: a}
}

// Get returns the correlation bundle for (tenantID, filter). Returns an
// empty bundle when no events match.
func (c *Correlator) Get(ctx context.Context, tenantID string, f CorrelationFilter) (*Bundle, error) {
	if c == nil || c.audit == nil {
		return nil, errors.New("playground: correlator not configured")
	}
	if tenantID == "" {
		return nil, errors.New("playground: tenant_id required")
	}
	// Pull the recent slice of audit events for this tenant; filter by
	// `meta.playground_session` in-memory. Audit's Query API doesn't
	// expose payload-level filters and the playground produces small
	// volumes per session, so the slice scan is fine.
	events, _, err := c.audit.Query(ctx, audit.Query{
		TenantID: tenantID,
		Since:    f.Since,
		Limit:    500,
	})
	if err != nil {
		return nil, err
	}

	bundle := &Bundle{
		SessionID:   f.SessionID,
		GeneratedAt: time.Now().UTC(),
	}

	var lastAt time.Time
	for _, ev := range events {
		if f.SessionID != "" && !eventMatchesSession(ev, f.SessionID) {
			continue
		}
		if ev.OccurredAt.After(lastAt) {
			lastAt = ev.OccurredAt
		}
		bundle.Audits = append(bundle.Audits, ev)
		if span, ok := spanFromEvent(ev); ok {
			bundle.Spans = append(bundle.Spans, span)
		}
		if pol, ok := policyFromEvent(ev); ok {
			bundle.Policy = append(bundle.Policy, pol)
		}
		if isDriftEvent(ev) {
			bundle.Drift = append(bundle.Drift, ev)
		}
	}
	if !lastAt.IsZero() {
		bundle.LastEventAge = time.Since(lastAt).Round(time.Millisecond).String()
	}
	return bundle, nil
}

// eventMatchesSession reports whether the event was emitted within the
// playground session id (carried in payload.meta or session_id).
func eventMatchesSession(ev audit.Event, sid string) bool {
	if ev.SessionID == sid {
		return true
	}
	if ev.Payload == nil {
		return false
	}
	if v, ok := ev.Payload["playground_session"].(string); ok && v == sid {
		return true
	}
	if meta, ok := ev.Payload["meta"].(map[string]any); ok {
		if v, ok := meta["playground_session"].(string); ok && v == sid {
			return true
		}
	}
	return false
}

func spanFromEvent(ev audit.Event) (SpanNode, bool) {
	switch ev.Type {
	case audit.EventToolCallStart, audit.EventToolCallComplete, audit.EventToolCallFailed:
		// Each event becomes a synthetic span keyed on tool name. Trace-
		// lite per phase plan deviation: a richer spanstore is Phase 11.
		span := SpanNode{
			SpanID:     ev.SpanID,
			ParentID:   ev.TraceID,
			Name:       eventToSpanName(ev),
			StartedAt:  ev.OccurredAt,
			Status:     spanStatus(ev.Type),
			Attributes: spanAttrs(ev),
		}
		if span.SpanID == "" {
			span.SpanID = ev.Type + "@" + ev.OccurredAt.Format(time.RFC3339Nano)
		}
		return span, true
	}
	return SpanNode{}, false
}

func policyFromEvent(ev audit.Event) (PolicyDecisionLite, bool) {
	if ev.Type != audit.EventPolicyAllowed && ev.Type != audit.EventPolicyDenied {
		return PolicyDecisionLite{}, false
	}
	tool, _ := ev.Payload["tool"].(string)
	reason, _ := ev.Payload["reason"].(string)
	dec := "allowed"
	if ev.Type == audit.EventPolicyDenied {
		dec = "denied"
	}
	return PolicyDecisionLite{
		Tool:     tool,
		Decision: dec,
		Reason:   reason,
		Detail:   ev.Payload,
		At:       ev.OccurredAt,
	}, true
}

func isDriftEvent(ev audit.Event) bool {
	return strings.HasPrefix(ev.Type, "schema.drift") ||
		strings.HasPrefix(ev.Type, "snapshot.drift") ||
		ev.Type == "schema.drift"
}

func eventToSpanName(ev audit.Event) string {
	if tool, ok := ev.Payload["tool"].(string); ok && tool != "" {
		return tool
	}
	return ev.Type
}

func spanStatus(eventType string) string {
	switch eventType {
	case audit.EventToolCallFailed:
		return "error"
	case audit.EventToolCallComplete:
		return "ok"
	default:
		return "running"
	}
}

func spanAttrs(ev audit.Event) map[string]string {
	out := map[string]string{}
	if ev.Payload == nil {
		return out
	}
	for k, v := range ev.Payload {
		switch tv := v.(type) {
		case string:
			out[k] = tv
		case bool, int, int64, float64:
			// Stringify numerics for transport simplicity.
			out[k] = stringifyScalar(tv)
		}
	}
	return out
}

func stringifyScalar(v any) string {
	switch tv := v.(type) {
	case bool:
		if tv {
			return "true"
		}
		return "false"
	default:
		// Use a tiny stringification; the frontend doesn't need types.
		return ""
	}
}
