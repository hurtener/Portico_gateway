package api

import (
	"bufio"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/registry"
)

// Phase 9 server handlers: restart, patch (env + enabled), logs (SSE),
// health, create, plus activity wiring.

func TestRestartServer_HappyPath(t *testing.T) {
	d, em, _, _, _, _, runtime, _, reg, _ := testDeps(t)
	seedServer(t, reg, "srv1")
	r := newReq("POST", "/api/servers/srv1/restart", map[string]string{"reason": "manual"})
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(restartServerHandler(d), r)
	statusOK(t, w, 202)
	if !hasEvent(em, audit.EventServerRestarted) {
		t.Errorf("missing server.restarted event")
	}
	if len(runtime.restarts) != 1 || runtime.restarts[0].reason != "manual" {
		t.Errorf("RecordRestart not called: %+v", runtime.restarts)
	}
}

func TestRestartServer_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/api/servers/ghost/restart", map[string]string{})
	r = withChiURLParam(r, "id", "ghost")
	w := runHandler(restartServerHandler(d), r)
	statusOK(t, w, 404)
}

func TestRestartServer_NoBody(t *testing.T) {
	d, _, _, _, _, _, _, _, reg, _ := testDeps(t)
	seedServer(t, reg, "srv1")
	r := newReq("POST", "/api/servers/srv1/restart", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(restartServerHandler(d), r)
	statusOK(t, w, 202)
}

func TestRestartServer_NoRegistry(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.Registry = nil
	r := newReq("POST", "/api/servers/srv1/restart", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(restartServerHandler(d), r)
	statusOK(t, w, 503)
}

func TestPatchServer_EnabledToggle(t *testing.T) {
	d, em, _, _, _, _, _, _, reg, _ := testDeps(t)
	seedServer(t, reg, "srv1")
	enabled := false
	body := map[string]any{"enabled": enabled}
	r := newReq("PATCH", "/api/servers/srv1", body)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(patchServerHandler(d), r)
	statusOK(t, w, 200)
	got, _ := reg.Get(context.Background(), "t1", "srv1")
	if got.Record.Enabled {
		t.Errorf("expected disabled, got enabled")
	}
	if !hasEvent(em, audit.EventServerUpdated) {
		t.Errorf("missing server.updated event")
	}
}

func TestPatchServer_EnvOverrides(t *testing.T) {
	d, _, _, _, _, _, runtime, _, reg, _ := testDeps(t)
	seedServer(t, reg, "srv1")
	body := map[string]any{"env_overrides": map[string]string{"DEBUG": "1"}}
	r := newReq("PATCH", "/api/servers/srv1", body)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(patchServerHandler(d), r)
	statusOK(t, w, 200)
	if rec, _ := runtime.Get(context.Background(), "t1", "srv1"); rec == nil || len(rec.EnvOverrides) == 0 {
		t.Errorf("env overrides not persisted")
	}
}

func TestPatchServer_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("PATCH", "/api/servers/ghost", map[string]any{})
	r = withChiURLParam(r, "id", "ghost")
	w := runHandler(patchServerHandler(d), r)
	statusOK(t, w, 404)
}

func TestPatchServer_BadJSON(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("PATCH", "/api/servers/srv1", nil)
	r.Body = httpReadCloser("not-json")
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(patchServerHandler(d), r)
	statusOK(t, w, 400)
}

func TestPatchServer_NoRegistry(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.Registry = nil
	r := newReq("PATCH", "/api/servers/srv1", map[string]any{})
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(patchServerHandler(d), r)
	statusOK(t, w, 503)
}

func TestHealthServer_HappyPath(t *testing.T) {
	d, _, _, _, _, _, _, _, reg, _ := testDeps(t)
	seedServer(t, reg, "srv1")
	r := newReq("GET", "/api/servers/srv1/health", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(healthServerHandler(d), r)
	statusOK(t, w, 200)
	var body map[string]any
	decodeJSON(t, w, &body)
	if body["server_id"] != "srv1" {
		t.Errorf("expected server_id=srv1, got %+v", body)
	}
}

func TestHealthServer_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("GET", "/api/servers/ghost/health", nil)
	r = withChiURLParam(r, "id", "ghost")
	w := runHandler(healthServerHandler(d), r)
	statusOK(t, w, 404)
}

func TestHealthServer_NoRegistry(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.Registry = nil
	r := newReq("GET", "/api/servers/srv1/health", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(healthServerHandler(d), r)
	statusOK(t, w, 503)
}

func TestLogsServer_StreamsAndCloses(t *testing.T) {
	d, _, _, _, _, _, _, _, reg, _ := testDeps(t)
	seedServer(t, reg, "srv1")
	r := newReq("GET", "/api/servers/srv1/logs", nil)
	r = withChiURLParam(r, "id", "srv1")
	// Use a context that we can cancel to make the handler return.
	ctx, cancel := context.WithTimeout(r.Context(), 250*time.Millisecond)
	defer cancel()
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	logsServerHandler(d).ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status: want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("expected SSE content-type, got %q", got)
	}
	// The handler should write at least one SSE event before blocking.
	scanner := bufio.NewScanner(strings.NewReader(w.Body.String()))
	saw := false
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "event:") || strings.HasPrefix(scanner.Text(), ":") {
			saw = true
		}
	}
	if !saw {
		t.Errorf("expected SSE event in body, got %q", w.Body.String())
	}
}

func TestLogsServer_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("GET", "/api/servers/ghost/logs", nil)
	r = withChiURLParam(r, "id", "ghost")
	w := runHandler(logsServerHandler(d), r)
	statusOK(t, w, 404)
}

func TestLogsServer_NoRegistry(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.Registry = nil
	r := newReq("GET", "/api/servers/srv1/logs", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(logsServerHandler(d), r)
	statusOK(t, w, 503)
}

func TestCreateAPIServer_HappyPath(t *testing.T) {
	d, em, _, _, _, _, _, _, _, _ := testDeps(t)
	spec := validServerSpec("new-srv")
	r := newReq("POST", "/api/servers", spec)
	w := runHandler(createAPIServerHandler(d), r)
	statusOK(t, w, 201)
	if !hasEvent(em, audit.EventServerCreated) {
		t.Errorf("missing server.created event")
	}
}

func TestCreateAPIServer_DuplicateRejected(t *testing.T) {
	d, _, _, _, _, _, _, _, reg, _ := testDeps(t)
	seedServer(t, reg, "dup")
	r := newReq("POST", "/api/servers", validServerSpec("dup"))
	w := runHandler(createAPIServerHandler(d), r)
	statusOK(t, w, 400)
}

func TestCreateAPIServer_ValidationFails(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	bad := &registry.ServerSpec{ID: ""} // empty id triggers FieldError
	r := newReq("POST", "/api/servers", bad)
	w := runHandler(createAPIServerHandler(d), r)
	statusOK(t, w, 400)
}

func TestCreateAPIServer_BadJSON(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/api/servers", nil)
	r.Body = httpReadCloser("not-json")
	w := runHandler(createAPIServerHandler(d), r)
	statusOK(t, w, 400)
}

func TestCreateAPIServer_NoRegistry(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.Registry = nil
	r := newReq("POST", "/api/servers", validServerSpec("x"))
	w := runHandler(createAPIServerHandler(d), r)
	statusOK(t, w, 503)
}

// activityHandler is shared across server / tenant / secret. Tests that the
// projection is read out and rendered as the canonical empty-array shape.
func TestActivityHandler_HappyPath(t *testing.T) {
	d, _, _, activity, _, _, _, _, _, _ := testDeps(t)
	rec := activityRecordForServer("srv1")
	if err := activity.Append(context.Background(), &rec); err != nil {
		t.Fatal(err)
	}
	r := newReq("GET", "/api/servers/srv1/activity", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(activityHandler(d, "server"), r)
	statusOK(t, w, 200)
	var rows []map[string]any
	decodeJSON(t, w, &rows)
	if len(rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(rows))
	}
}

func TestActivityHandler_NoStore(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.EntityActivity = nil
	r := newReq("GET", "/api/servers/srv1/activity", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(activityHandler(d, "server"), r)
	statusOK(t, w, 200)
}

func TestActivityHandler_LimitClamping(t *testing.T) {
	d, _, _, activity, _, _, _, _, _, _ := testDeps(t)
	for i := 0; i < 3; i++ {
		rec := activityRecordForServer("srv1")
		_ = activity.Append(context.Background(), &rec)
	}
	r := newReq("GET", "/api/servers/srv1/activity?limit=2", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(activityHandler(d, "server"), r)
	statusOK(t, w, 200)
	var rows []map[string]any
	decodeJSON(t, w, &rows)
	if len(rows) != 2 {
		t.Errorf("expected limit=2, got %d", len(rows))
	}
}

// activityHandler also reads the {name} URL param when {id} is absent so
// the same handler can mount under /api/admin/secrets/{name}/activity.
func TestActivityHandler_SecretsNameParam(t *testing.T) {
	d, _, _, activity, _, _, _, _, _, _ := testDeps(t)
	rec := activityRecordForSecret("k")
	if err := activity.Append(context.Background(), &rec); err != nil {
		t.Fatal(err)
	}
	r := newReq("GET", "/api/admin/secrets/k/activity", nil)
	r = withChiURLParam(r, "name", "k")
	w := runHandler(activityHandler(d, "secret"), r)
	statusOK(t, w, 200)
	var rows []map[string]any
	decodeJSON(t, w, &rows)
	if len(rows) != 1 {
		t.Errorf("expected 1 row from secret/k, got %d", len(rows))
	}
}
