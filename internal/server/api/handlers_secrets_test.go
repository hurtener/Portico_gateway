package api

import (
	"net/http/httptest"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Phase 9 secrets surface: list, create, get-metadata, put, delete, rotate,
// reveal issue + consume, plus permission-denied checks.

func TestListAdminSecrets_BackCompat(t *testing.T) {
	d, _, tenants, _, vault, _, _, _, _, _ := testDeps(t)
	tenants.tenants["t1"] = &ifaces.Tenant{ID: "t1", DisplayName: "Acme", Plan: "pro"}
	if err := vault.Put(t.Context(), "t1", "s1", "v1"); err != nil {
		t.Fatal(err)
	}
	r := newReq("GET", "/v1/admin/secrets", nil)
	w := runHandler(listAdminSecretsHandler(d), r)
	statusOK(t, w, 200)
}

func TestListAdminSecrets_VaultMissing(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.Vault = nil
	r := newReq("GET", "/v1/admin/secrets", nil)
	w := runHandler(listAdminSecretsHandler(d), r)
	statusOK(t, w, 503)
}

func TestPutAdminSecret_HappyPath(t *testing.T) {
	d, em, _, _, vault, _, _, _, _, _ := testDeps(t)
	r := newReq("PUT", "/v1/admin/secrets/t1/k1", map[string]string{"value": "v1"})
	r = withChiURLParam(r, "tenant", "t1")
	r = withChiURLParam(r, "name", "k1")
	w := runHandler(putAdminSecretHandler(d), r)
	statusOK(t, w, 204)
	if got, _ := vault.Get(r.Context(), "t1", "k1"); got != "v1" {
		t.Errorf("vault did not record value: %q", got)
	}
	if !hasEvent(em, audit.EventSecretUpdated) {
		t.Errorf("missing secret.updated event")
	}
}

func TestPutAdminSecret_RequiresValue(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("PUT", "/v1/admin/secrets/t1/k1", map[string]string{"value": ""})
	r = withChiURLParam(r, "tenant", "t1")
	r = withChiURLParam(r, "name", "k1")
	w := runHandler(putAdminSecretHandler(d), r)
	statusOK(t, w, 400)
}

func TestPutAdminSecret_InvalidJSON(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("PUT", "/v1/admin/secrets/t1/k1", nil)
	r.Body = httpReadCloser("not-json")
	r = withChiURLParam(r, "tenant", "t1")
	r = withChiURLParam(r, "name", "k1")
	w := runHandler(putAdminSecretHandler(d), r)
	statusOK(t, w, 400)
}

func TestDeleteAdminSecret_HappyPath(t *testing.T) {
	d, em, _, _, vault, _, _, _, _, _ := testDeps(t)
	if err := vault.Put(t.Context(), "t1", "k1", "v"); err != nil {
		t.Fatal(err)
	}
	r := newReq("DELETE", "/v1/admin/secrets/t1/k1", nil)
	r = withChiURLParam(r, "tenant", "t1")
	r = withChiURLParam(r, "name", "k1")
	w := runHandler(deleteAdminSecretHandler(d), r)
	statusOK(t, w, 204)
	if !hasEvent(em, audit.EventSecretDeleted) {
		t.Errorf("missing secret.deleted event")
	}
}

func TestDeleteAdminSecret_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("DELETE", "/v1/admin/secrets/t1/missing", nil)
	r = withChiURLParam(r, "tenant", "t1")
	r = withChiURLParam(r, "name", "missing")
	w := runHandler(deleteAdminSecretHandler(d), r)
	statusOK(t, w, 404)
}

// ---- /api/admin/secrets surface --------------------------------------

func TestListAPISecrets_HappyPath(t *testing.T) {
	d, _, _, _, vault, _, _, _, _, _ := testDeps(t)
	_ = vault.Put(t.Context(), "t1", "alpha", "1")
	_ = vault.Put(t.Context(), "t1", "beta", "2")
	r := newReq("GET", "/api/admin/secrets", nil)
	w := runHandler(listAPISecretsHandler(d), r)
	statusOK(t, w, 200)
	var rows []map[string]any
	decodeJSON(t, w, &rows)
	if len(rows) != 2 {
		t.Errorf("want 2 secrets, got %d", len(rows))
	}
}

func TestListAPISecrets_AdminCrossTenantQuery(t *testing.T) {
	d, _, _, _, vault, _, _, _, _, _ := testDeps(t)
	_ = vault.Put(t.Context(), "other", "x", "1")
	r := newReq("GET", "/api/admin/secrets?tenant=other", nil)
	w := runHandler(listAPISecretsHandler(d), r)
	statusOK(t, w, 200)
	var rows []map[string]any
	decodeJSON(t, w, &rows)
	if len(rows) != 1 {
		t.Errorf("expected admin to see other tenant's secrets")
	}
}

func TestListAPISecrets_VaultMissing(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.Vault = nil
	r := newReq("GET", "/api/admin/secrets", nil)
	w := runHandler(listAPISecretsHandler(d), r)
	statusOK(t, w, 503)
}

func TestCreateAPISecret_HappyPath(t *testing.T) {
	d, em, _, _, vault, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/api/admin/secrets", map[string]string{
		"tenant_id": "t1", "name": "secret-x", "value": "yo",
	})
	w := runHandler(createAPISecretHandler(d), r)
	statusOK(t, w, 201)
	if got, _ := vault.Get(r.Context(), "t1", "secret-x"); got != "yo" {
		t.Errorf("vault entry missing")
	}
	if !hasEvent(em, audit.EventSecretCreated) {
		t.Errorf("missing secret.created event")
	}
}

func TestCreateAPISecret_ValidationFails(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/api/admin/secrets", map[string]string{
		"tenant_id": "t1", "name": "", "value": "x",
	})
	w := runHandler(createAPISecretHandler(d), r)
	statusOK(t, w, 400)
}

func TestCreateAPISecret_BadJSON(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/api/admin/secrets", nil)
	r.Body = httpReadCloser("not-json")
	w := runHandler(createAPISecretHandler(d), r)
	statusOK(t, w, 400)
}

func TestCreateAPISecret_CrossTenantWithoutAdminDenied(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/api/admin/secrets",
		map[string]string{"tenant_id": "other", "name": "n", "value": "v"},
		"viewer")
	w := runHandler(createAPISecretHandler(d), r)
	statusOK(t, w, 403)
}

func TestGetAPISecretMetadata_HappyPath(t *testing.T) {
	d, _, _, _, vault, _, _, _, _, _ := testDeps(t)
	_ = vault.Put(t.Context(), "t1", "k", "v")
	r := newReq("GET", "/api/admin/secrets/k", nil)
	r = withChiURLParam(r, "name", "k")
	w := runHandler(getAPISecretMetadataHandler(d), r)
	statusOK(t, w, 200)
}

func TestGetAPISecretMetadata_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("GET", "/api/admin/secrets/missing", nil)
	r = withChiURLParam(r, "name", "missing")
	w := runHandler(getAPISecretMetadataHandler(d), r)
	statusOK(t, w, 404)
}

func TestPutAPISecret_HappyPath(t *testing.T) {
	d, em, _, _, vault, _, _, _, _, _ := testDeps(t)
	_ = vault.Put(t.Context(), "t1", "k", "old")
	r := newReq("PUT", "/api/admin/secrets/k", map[string]string{"value": "new"})
	r = withChiURLParam(r, "name", "k")
	w := runHandler(putAPISecretHandler(d), r)
	statusOK(t, w, 200)
	if got, _ := vault.Get(r.Context(), "t1", "k"); got != "new" {
		t.Errorf("value not updated")
	}
	if !hasEvent(em, audit.EventSecretUpdated) {
		t.Errorf("missing secret.updated event")
	}
}

func TestPutAPISecret_ValidationFails(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("PUT", "/api/admin/secrets/k", map[string]string{"value": ""})
	r = withChiURLParam(r, "name", "k")
	w := runHandler(putAPISecretHandler(d), r)
	statusOK(t, w, 400)
}

func TestPutAPISecret_BadJSON(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("PUT", "/api/admin/secrets/k", nil)
	r.Body = httpReadCloser("not-json")
	r = withChiURLParam(r, "name", "k")
	w := runHandler(putAPISecretHandler(d), r)
	statusOK(t, w, 400)
}

func TestDeleteAPISecret_HappyPath(t *testing.T) {
	d, em, _, _, vault, _, _, _, _, _ := testDeps(t)
	_ = vault.Put(t.Context(), "t1", "k", "v")
	r := newReq("DELETE", "/api/admin/secrets/k", nil)
	r = withChiURLParam(r, "name", "k")
	w := runHandler(deleteAPISecretHandler(d), r)
	statusOK(t, w, 204)
	if !hasEvent(em, audit.EventSecretDeleted) {
		t.Errorf("missing secret.deleted event")
	}
}

func TestDeleteAPISecret_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("DELETE", "/api/admin/secrets/ghost", nil)
	r = withChiURLParam(r, "name", "ghost")
	w := runHandler(deleteAPISecretHandler(d), r)
	statusOK(t, w, 404)
}

func TestRotateAPISecret_HappyPath(t *testing.T) {
	d, em, _, _, vault, _, _, _, _, _ := testDeps(t)
	_ = vault.Put(t.Context(), "t1", "k", "v")
	r := newReq("POST", "/api/admin/secrets/k/rotate", nil)
	r = withChiURLParam(r, "name", "k")
	w := runHandler(rotateAPISecretHandler(d), r)
	statusOK(t, w, 200)
	if !hasEvent(em, audit.EventSecretRotated) {
		t.Errorf("missing secret.rotated event")
	}
}

func TestRotateAPISecret_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/api/admin/secrets/missing/rotate", nil)
	r = withChiURLParam(r, "name", "missing")
	w := runHandler(rotateAPISecretHandler(d), r)
	statusOK(t, w, 404)
}

func TestRevealIssue_HappyPath(t *testing.T) {
	d, em, _, _, vault, _, _, _, _, _ := testDeps(t)
	_ = vault.Put(t.Context(), "t1", "k", "v")
	r := newReq("POST", "/api/admin/secrets/k/reveal", nil)
	r = withChiURLParam(r, "name", "k")
	w := runHandler(revealIssueHandler(d), r)
	statusOK(t, w, 200)
	if !hasEvent(em, audit.EventSecretRevealIssued) {
		t.Errorf("missing reveal.issued event")
	}
}

func TestRevealIssue_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/api/admin/secrets/ghost/reveal", nil)
	r = withChiURLParam(r, "name", "ghost")
	w := runHandler(revealIssueHandler(d), r)
	statusOK(t, w, 404)
}

func TestRevealIssue_NotConfigured(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.VaultReveal = nil
	r := newReq("POST", "/api/admin/secrets/k/reveal", nil)
	r = withChiURLParam(r, "name", "k")
	w := runHandler(revealIssueHandler(d), r)
	statusOK(t, w, 503)
}

func TestRevealConsume_HappyPath(t *testing.T) {
	d, em, _, _, vault, reveal, _, _, _, _ := testDeps(t)
	_ = vault.Put(t.Context(), "t1", "k", "secret-v")
	tok, err := reveal.IssueRevealToken(t.Context(), "t1", "k", "tester")
	if err != nil {
		t.Fatal(err)
	}
	r := newReq("GET", "/api/admin/secrets/reveal/"+tok.Token, nil)
	r = withChiURLParam(r, "token", tok.Token)
	w := runHandler(revealConsumeHandler(d), r)
	statusOK(t, w, 200)
	var body map[string]any
	decodeJSON(t, w, &body)
	if body["value"] != "secret-v" {
		t.Errorf("expected plaintext, got %+v", body)
	}
	if !hasEvent(em, audit.EventSecretRevealConsumed) {
		t.Errorf("missing reveal.consumed event")
	}
}

func TestRevealConsume_BadToken(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("GET", "/api/admin/secrets/reveal/bogus", nil)
	r = withChiURLParam(r, "token", "bogus")
	w := runHandler(revealConsumeHandler(d), r)
	statusOK(t, w, 400)
}

func TestRotateRoot_NotImplemented(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/api/admin/secrets/rotate-root", nil)
	w := runHandler(rotateRootHandler(d), r)
	statusOK(t, w, 501)
}

// Permission denied: scope.Require("admin") wraps the secrets routes.
func TestSecretHandlers_RequireAdminScope(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	id := tenant.Identity{TenantID: "t1", UserID: "u1", Scopes: []string{"viewer"}}
	wrapped := adminAuth(id, listAPISecretsHandler(d), true)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/admin/secrets", nil)
	wrapped.ServeHTTP(w, r)
	statusOK(t, w, 403)
}
