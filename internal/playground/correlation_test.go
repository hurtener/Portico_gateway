package playground

import (
	"context"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
)

type fakeAudit struct {
	events []audit.Event
}

func (f *fakeAudit) Query(_ context.Context, q audit.Query) ([]audit.Event, string, error) {
	out := make([]audit.Event, 0, len(f.events))
	for _, ev := range f.events {
		if q.Since.IsZero() || !ev.OccurredAt.Before(q.Since) {
			out = append(out, ev)
		}
	}
	return out, "", nil
}

func TestCorrelation_BundlesAllChannels(t *testing.T) {
	now := time.Now().UTC()
	a := &fakeAudit{events: []audit.Event{
		{Type: audit.EventToolCallStart, TenantID: "t", SessionID: "psn_1", OccurredAt: now, Payload: map[string]any{"tool": "github.list_repos"}},
		{Type: audit.EventPolicyAllowed, TenantID: "t", SessionID: "psn_1", OccurredAt: now, Payload: map[string]any{"tool": "github.list_repos"}},
		{Type: audit.EventToolCallComplete, TenantID: "t", SessionID: "psn_1", OccurredAt: now, Payload: map[string]any{"tool": "github.list_repos"}},
		{Type: "schema.drift", TenantID: "t", SessionID: "psn_1", OccurredAt: now, Payload: map[string]any{"server_id": "github"}},
		{Type: audit.EventToolCallStart, TenantID: "t", SessionID: "other", OccurredAt: now}, // out of session
	}}
	c := NewCorrelator(a)
	bundle, err := c.Get(context.Background(), "t", CorrelationFilter{SessionID: "psn_1"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(bundle.Audits) != 4 {
		t.Fatalf("expected 4 audits, got %d", len(bundle.Audits))
	}
	if len(bundle.Spans) < 2 {
		t.Fatalf("expected ≥2 spans (start/complete), got %d", len(bundle.Spans))
	}
	if len(bundle.Policy) != 1 {
		t.Fatalf("expected 1 policy decision, got %d", len(bundle.Policy))
	}
	if len(bundle.Drift) != 1 {
		t.Fatalf("expected 1 drift event, got %d", len(bundle.Drift))
	}
}

func TestCorrelation_Since_FiltersIncrementally(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-time.Hour)
	a := &fakeAudit{events: []audit.Event{
		{Type: audit.EventToolCallStart, TenantID: "t", SessionID: "s", OccurredAt: old},
		{Type: audit.EventToolCallComplete, TenantID: "t", SessionID: "s", OccurredAt: now},
	}}
	c := NewCorrelator(a)
	bundle, err := c.Get(context.Background(), "t", CorrelationFilter{SessionID: "s", Since: now.Add(-time.Minute)})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(bundle.Audits) != 1 {
		t.Fatalf("expected 1 audit since now-1min, got %d", len(bundle.Audits))
	}
}

func TestCorrelation_MatchesViaPayloadMeta(t *testing.T) {
	now := time.Now().UTC()
	a := &fakeAudit{events: []audit.Event{
		{Type: audit.EventToolCallStart, TenantID: "t", OccurredAt: now,
			Payload: map[string]any{"meta": map[string]any{"playground_session": "psn_1"}}},
	}}
	c := NewCorrelator(a)
	bundle, err := c.Get(context.Background(), "t", CorrelationFilter{SessionID: "psn_1"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(bundle.Audits) != 1 {
		t.Fatalf("expected 1 audit; meta-based match failed; got %d", len(bundle.Audits))
	}
}

func TestCorrelation_Redaction_Honored(t *testing.T) {
	// Redaction happens at the audit emit path; the correlator just
	// surfaces what the audit Query returns. This test asserts the
	// correlator does NOT reach into Payload to leak redacted fields.
	now := time.Now().UTC()
	a := &fakeAudit{events: []audit.Event{
		{Type: audit.EventToolCallStart, TenantID: "t", SessionID: "s", OccurredAt: now,
			Payload: map[string]any{"redacted_token": "[REDACTED]"}},
	}}
	c := NewCorrelator(a)
	bundle, err := c.Get(context.Background(), "t", CorrelationFilter{SessionID: "s"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := bundle.Audits[0].Payload["redacted_token"].(string)
	if got != "[REDACTED]" {
		t.Fatalf("correlator must surface payload as-is")
	}
}

func TestCorrelation_RejectsEmptyTenant(t *testing.T) {
	c := NewCorrelator(&fakeAudit{})
	if _, err := c.Get(context.Background(), "", CorrelationFilter{}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestCorrelation_NilCorrelator(t *testing.T) {
	var c *Correlator
	if _, err := c.Get(context.Background(), "t", CorrelationFilter{}); err == nil {
		t.Fatalf("expected error from nil correlator")
	}
}

func TestCorrelation_DirectSessionIDMatch(t *testing.T) {
	now := time.Now().UTC()
	a := &fakeAudit{events: []audit.Event{
		{Type: audit.EventToolCallFailed, TenantID: "t", SessionID: "psn_1", OccurredAt: now,
			Payload: map[string]any{"tool": "x.y", "playground_session": "psn_1"}},
	}}
	c := NewCorrelator(a)
	bundle, err := c.Get(context.Background(), "t", CorrelationFilter{SessionID: "psn_1"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(bundle.Spans) != 1 || bundle.Spans[0].Status != "error" {
		t.Fatalf("expected one error span, got %+v", bundle.Spans)
	}
}
