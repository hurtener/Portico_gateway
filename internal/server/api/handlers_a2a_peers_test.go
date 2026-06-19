package api

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

// newA2ARouterDeps builds a Deps with DevMode tenant "t1" + admin scope, the
// in-memory A2A peer store wired, and mounts NewRouter. Returns the mounted
// handler so each test can issue real HTTP requests.
func newA2ARouterDeps(t *testing.T) (http.Handler, *sqlite.DB) {
	t.Helper()
	db := openGovDB(t)
	d := Deps{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		DevMode:   true,
		DevTenant: "t1",
		A2APeers:  db.A2APeers(),
	}
	return NewRouter(d), db
}

// TestA2APeers_CRUD covers create→get→list→update→delete for A2A peers,
// plus 404 on a missing get/update/delete.
func TestA2APeers_CRUD(t *testing.T) {
	h, _ := newA2ARouterDeps(t)

	// Create.
	w := doReq(t, h, "POST", "/api/a2a/peers", map[string]any{
		"name":            "research-agent",
		"endpoint":        "https://peer.example/a2a",
		"egress_auth_ref": "",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (%s)", w.Code, w.Body.String())
	}
	var created A2APeerDTO
	govDecode(t, w, &created)
	if created.ID == "" || created.Name != "research-agent" || created.Endpoint != "https://peer.example/a2a" {
		t.Fatalf("created DTO mismatch: %+v", created)
	}
	if !created.Enabled {
		t.Fatalf("Enabled default: want true, got false (%+v)", created)
	}
	if created.CreatedAt == "" || created.UpdatedAt == "" {
		t.Fatalf("timestamps not populated: %+v", created)
	}
	if len(created.ID) < 6 || created.ID[:4] != "a2a_" {
		t.Fatalf("id not a2a_-prefixed: %s", created.ID)
	}

	// Get.
	w = doReq(t, h, "GET", "/api/a2a/peers/"+created.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var got A2APeerDTO
	govDecode(t, w, &got)
	if got.ID != created.ID || got.Name != "research-agent" {
		t.Fatalf("get mismatch: %+v", got)
	}

	// List includes it.
	w = doReq(t, h, "GET", "/api/a2a/peers", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var list []A2APeerDTO
	govDecode(t, w, &list)
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list mismatch: %+v", list)
	}

	// Update — change name; leave Endpoint untouched.
	w = doReq(t, h, "PUT", "/api/a2a/peers/"+created.ID, map[string]any{
		"name":     "research-agent-2",
		"endpoint": "",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var updated A2APeerDTO
	govDecode(t, w, &updated)
	if updated.Name != "research-agent-2" {
		t.Fatalf("update did not change name: %+v", updated)
	}
	if updated.Endpoint != "https://peer.example/a2a" {
		t.Fatalf("update wiped Endpoint: %+v", updated)
	}
	if updated.CreatedAt != got.CreatedAt {
		t.Fatalf("created_at changed on update: was %s now %s", got.CreatedAt, updated.CreatedAt)
	}

	// Delete → 204; subsequent get → 404.
	w = doReq(t, h, "DELETE", "/api/a2a/peers/"+created.ID, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204 (%s)", w.Code, w.Body.String())
	}
	w = doReq(t, h, "GET", "/api/a2a/peers/"+created.ID, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get-after-delete status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
	if readErrorCode(t, w) != "not_found" {
		t.Fatalf("error code: want not_found, got %q", readErrorCode(t, w))
	}
}

// TestA2APeers_CreateRequiresFields covers the 400 path: name AND endpoint
// are required.
func TestA2APeers_CreateRequiresFields(t *testing.T) {
	h, _ := newA2ARouterDeps(t)
	w := doReq(t, h, "POST", "/api/a2a/peers", map[string]any{"name": "no-endpoint"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create without endpoint status = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	if readErrorCode(t, w) != "invalid_request" {
		t.Fatalf("error code: want invalid_request, got %q", readErrorCode(t, w))
	}
	w = doReq(t, h, "POST", "/api/a2a/peers", map[string]any{"endpoint": "https://peer.example/a2a"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create without name status = %d, want 400 (%s)", w.Code, w.Body.String())
	}
}

// TestA2APeers_GetNotFound covers 404 on get.
func TestA2APeers_GetNotFound(t *testing.T) {
	h, _ := newA2ARouterDeps(t)
	w := doReq(t, h, "GET", "/api/a2a/peers/a2a_nope", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get missing status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
	if readErrorCode(t, w) != "not_found" {
		t.Fatalf("error code: want not_found, got %q", readErrorCode(t, w))
	}
}

// TestA2APeers_UpdateNotFound covers 404 on update.
func TestA2APeers_UpdateNotFound(t *testing.T) {
	h, _ := newA2ARouterDeps(t)
	w := doReq(t, h, "PUT", "/api/a2a/peers/a2a_nope", map[string]any{"name": "x"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("update missing status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
}

// TestA2APeers_DeleteNotFound covers 404 on delete.
func TestA2APeers_DeleteNotFound(t *testing.T) {
	h, _ := newA2ARouterDeps(t)
	w := doReq(t, h, "DELETE", "/api/a2a/peers/a2a_nope", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("delete missing status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
}

// TestA2APeers_NilStore503 verifies the handlers degrade to 503 when the A2A
// peer store is not wired. Mirrors TestGovernanceCRUD_NilStores503: invoke
// the handler directly, since NewRouter skips mounting the routes when the
// store is nil.
func TestA2APeers_NilStore503(t *testing.T) {
	w := runHandler(listA2APeersHandler(Deps{}), newReq("GET", "/api/a2a/peers", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", w.Code, w.Body.String())
	}
	if readErrorCode(t, w) != "a2a_not_configured" {
		t.Fatalf("error code: want a2a_not_configured, got %q", readErrorCode(t, w))
	}
}
