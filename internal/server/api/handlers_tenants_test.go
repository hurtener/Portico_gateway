package api

import (
	"net/http/httptest"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Phase 9 tenant CRUD: list, get, upsert (POST + PUT), delete (archive),
// purge, plus activity. Tests cover happy-path, validation, 403, 404, and
// audit emission.

func TestListTenants_HappyPath(t *testing.T) {
	d, _, tenants, _, _, _, _, _, _, _ := testDeps(t)
	tenants.tenants["t1"] = &ifaces.Tenant{ID: "t1", DisplayName: "Acme", Plan: "pro"}
	tenants.tenants["t2"] = &ifaces.Tenant{ID: "t2", DisplayName: "Bravo", Plan: "free"}

	r := newReq("GET", "/api/admin/tenants", nil)
	w := runHandler(listTenantsHandler(d), r)
	statusOK(t, w, 200)
	var got []map[string]any
	decodeJSON(t, w, &got)
	if len(got) != 2 {
		t.Errorf("want 2 tenants, got %d", len(got))
	}
}

func TestGetTenant_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("GET", "/api/admin/tenants/missing", nil)
	r = withChiURLParam(r, "id", "missing")
	w := runHandler(getTenantHandler(d), r)
	statusOK(t, w, 404)
	if got := readErrorCode(t, w); got != "not_found" {
		t.Errorf("want not_found, got %q", got)
	}
}

func TestUpsertTenant_Create_HappyPath(t *testing.T) {
	d, em, tenants, _, _, _, _, _, _, _ := testDeps(t)
	body := tenantWriteBody{ID: "newco", DisplayName: "New Co", Plan: "pro"}
	r := newReq("POST", "/api/admin/tenants", body)
	w := runHandler(upsertTenantHandler(d, false), r)
	statusOK(t, w, 201)
	if _, ok := tenants.tenants["newco"]; !ok {
		t.Errorf("tenant was not stored")
	}
	if !hasEvent(em, audit.EventTenantCreated) {
		t.Errorf("missing tenant.created event in %+v", em.Events())
	}
}

func TestUpsertTenant_Update_HappyPath(t *testing.T) {
	d, em, tenants, _, _, _, _, _, _, _ := testDeps(t)
	tenants.tenants["acme"] = &ifaces.Tenant{ID: "acme", DisplayName: "old", Plan: "free"}
	body := tenantWriteBody{ID: "acme", DisplayName: "New Name", Plan: "pro"}
	r := newReq("PUT", "/api/admin/tenants/acme", body)
	r = withChiURLParam(r, "id", "acme")
	w := runHandler(upsertTenantHandler(d, true), r)
	statusOK(t, w, 200)
	if !hasEvent(em, audit.EventTenantUpdated) {
		t.Errorf("missing tenant.updated event")
	}
}

func TestUpsertTenant_PutOnMissing_404(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	body := tenantWriteBody{ID: "ghost", DisplayName: "G", Plan: "pro"}
	r := newReq("PUT", "/api/admin/tenants/ghost", body)
	r = withChiURLParam(r, "id", "ghost")
	w := runHandler(upsertTenantHandler(d, true), r)
	statusOK(t, w, 404)
}

func TestUpsertTenant_ValidationFails(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	// Missing display_name is a validation error.
	body := tenantWriteBody{ID: "x", Plan: "pro"}
	r := newReq("POST", "/api/admin/tenants", body)
	w := runHandler(upsertTenantHandler(d, false), r)
	statusOK(t, w, 400)
	if got := readErrorCode(t, w); got != "validation_failed" {
		t.Errorf("want validation_failed, got %q", got)
	}
}

func TestUpsertTenant_PutIDMismatch(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	body := tenantWriteBody{ID: "other", DisplayName: "O", Plan: "pro"}
	r := newReq("PUT", "/api/admin/tenants/wanted", body)
	r = withChiURLParam(r, "id", "wanted")
	w := runHandler(upsertTenantHandler(d, true), r)
	statusOK(t, w, 400)
	if got := readErrorCode(t, w); got != "id_mismatch" {
		t.Errorf("want id_mismatch, got %q", got)
	}
}

func TestUpsertTenant_BadJSON(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	// Pass an invalid JSON body.
	r := newReq("POST", "/api/admin/tenants", nil)
	r.Body = httpReadCloser("{not-json")
	w := runHandler(upsertTenantHandler(d, false), r)
	statusOK(t, w, 400)
}

func TestUpsertTenant_StatusValidation(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	body := tenantWriteBody{ID: "x", DisplayName: "X", Plan: "pro", Status: "weird"}
	r := newReq("POST", "/api/admin/tenants", body)
	w := runHandler(upsertTenantHandler(d, false), r)
	statusOK(t, w, 400)
}

func TestDeleteTenant_ArchivesAndEmits(t *testing.T) {
	d, em, tenants, activity, _, _, _, _, _, _ := testDeps(t)
	tenants.tenants["doomed"] = &ifaces.Tenant{ID: "doomed", DisplayName: "D", Plan: "free"}
	r := newReq("DELETE", "/api/admin/tenants/doomed", nil)
	r = withChiURLParam(r, "id", "doomed")
	w := runHandler(deleteTenantHandler(d), r)
	statusOK(t, w, 204)
	got, _ := tenants.Get(r.Context(), "doomed")
	if got == nil || got.Status != "archived" {
		t.Errorf("expected archived status, got %+v", got)
	}
	if !hasEvent(em, "tenant.archived") {
		t.Errorf("missing tenant.archived event")
	}
	rows := activity.rowsCopy()
	if len(rows) == 0 {
		t.Errorf("expected entity_activity row to be appended")
	}
}

func TestDeleteTenant_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("DELETE", "/api/admin/tenants/ghost", nil)
	r = withChiURLParam(r, "id", "ghost")
	w := runHandler(deleteTenantHandler(d), r)
	statusOK(t, w, 404)
}

func TestPurgeTenant_DeletesAndEmits(t *testing.T) {
	d, em, tenants, _, _, _, _, _, _, _ := testDeps(t)
	tenants.tenants["dead"] = &ifaces.Tenant{ID: "dead", DisplayName: "D", Plan: "free"}
	r := newReq("POST", "/api/admin/tenants/dead/purge", nil)
	r = withChiURLParam(r, "id", "dead")
	w := runHandler(purgeTenantHandler(d), r)
	statusOK(t, w, 204)
	if _, ok := tenants.tenants["dead"]; ok {
		t.Errorf("tenant should be hard-deleted")
	}
	if !hasEvent(em, "tenant.purged") {
		t.Errorf("missing tenant.purged event")
	}
}

func TestPurgeTenant_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/api/admin/tenants/ghost/purge", nil)
	r = withChiURLParam(r, "id", "ghost")
	w := runHandler(purgeTenantHandler(d), r)
	statusOK(t, w, 404)
}

// Permission denied: the production router wraps these handlers with
// scope.Require("admin"). We test the wrapping path directly so the
// authorization contract is exercised.
func TestTenantHandlers_RequireAdminScope(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	id := tenant.Identity{TenantID: "t1", UserID: "u1", Scopes: []string{"viewer"}}
	wrapped := adminAuth(id, listTenantsHandler(d), true)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/admin/tenants", nil)
	wrapped.ServeHTTP(w, r)
	statusOK(t, w, 403)
}
