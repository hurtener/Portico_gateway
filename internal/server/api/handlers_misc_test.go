package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Coverage for the small handlers — health, readyz, audit query, 404/405,
// and the writeJSON helpers that backstop them.

func TestHealthzHandler(t *testing.T) {
	r := httptest.NewRequest("GET", "/healthz", nil)
	w := runHandler(http.HandlerFunc(healthzHandler), r)
	statusOK(t, w, 200)
}

func TestReadyzHandler_DefaultsVersion(t *testing.T) {
	d := Deps{}
	r := httptest.NewRequest("GET", "/readyz", nil)
	w := runHandler(readyzHandler(d), r)
	statusOK(t, w, 200)
	var body map[string]any
	decodeJSON(t, w, &body)
	if body["version"] != "v0.0.0" {
		t.Errorf("expected default version, got %+v", body)
	}
}

func TestReadyzHandler_KeepsConfiguredVersion(t *testing.T) {
	d := Deps{Version: "v1.2.3", BuildCommit: "deadbeef"}
	r := httptest.NewRequest("GET", "/readyz", nil)
	w := runHandler(readyzHandler(d), r)
	statusOK(t, w, 200)
	var body map[string]any
	decodeJSON(t, w, &body)
	if body["version"] != "v1.2.3" || body["commit"] != "deadbeef" {
		t.Errorf("readyz body: %+v", body)
	}
}

func TestNotFoundHandler(t *testing.T) {
	r := httptest.NewRequest("GET", "/nope", nil)
	w := runHandler(http.HandlerFunc(notFoundHandler), r)
	statusOK(t, w, 404)
}

func TestMethodNotAllowedHandler(t *testing.T) {
	r := httptest.NewRequest("PATCH", "/healthz", nil)
	w := runHandler(http.HandlerFunc(methodNotAllowedHandler), r)
	statusOK(t, w, 405)
}

func TestAuditQuery_HappyPath(t *testing.T) {
	store := newStubAuditStore()
	_ = store.Append(context.Background(), &ifaces.AuditEvent{TenantID: "t1", Type: "x", OccurredAt: time.Now()})
	d := Deps{Audit: store}
	r := newReq("GET", "/v1/audit/events", nil)
	w := runHandler(auditQueryHandler(d), r)
	statusOK(t, w, 200)
}

func TestAuditQuery_WithFilters(t *testing.T) {
	store := newStubAuditStore()
	d := Deps{Audit: store}
	r := newReq("GET", "/v1/audit/events?type=foo,bar&since=2026-01-01T00:00:00Z&until=2026-12-31T00:00:00Z&limit=10&cursor=abc", nil)
	w := runHandler(auditQueryHandler(d), r)
	statusOK(t, w, 200)
}

func TestAuditQuery_StoreError(t *testing.T) {
	store := newStubAuditStore()
	store.failQ = true
	d := Deps{Audit: store}
	r := newReq("GET", "/v1/audit/events", nil)
	w := runHandler(auditQueryHandler(d), r)
	statusOK(t, w, 500)
}

func TestParseLimit(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"", 100},
		{"abc", 100},
		{"-1", 100},
		{"50", 50},
		{"5000", 1000}, // clamped to max
	}
	for _, tc := range cases {
		if got := parseLimit(tc.raw, 100, 1000); got != tc.want {
			t.Errorf("parseLimit(%q): want %d got %d", tc.raw, tc.want, got)
		}
	}
}

func TestSlogRequestLogger_PassesThrough(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	mw := slogRequestLogger(d.Logger)
	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(204)
	}))
	r := httptest.NewRequest("GET", "/x", nil)
	w := runHandler(h, r)
	if !called {
		t.Errorf("inner handler not invoked")
	}
	statusOK(t, w, 204)
}
