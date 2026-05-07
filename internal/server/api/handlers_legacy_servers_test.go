package api

import (
	"net/http/httptest"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/registry"
)

// Coverage for the /v1/servers handlers reused from Phase 2. Keeps the
// package-level coverage above the Phase 9 gate without duplicating the
// Phase 2 unit tests in detail.

func TestListServers_HappyPath(t *testing.T) {
	d, _, _, _, _, _, _, _, reg, _ := testDeps(t)
	seedServer(t, reg, "a")
	seedServer(t, reg, "b")
	r := newReq("GET", "/v1/servers", nil)
	w := runHandler(listServersHandler(d), r)
	statusOK(t, w, 200)
}

func TestListServers_NoRegistry(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.Registry = nil
	r := newReq("GET", "/v1/servers", nil)
	w := runHandler(listServersHandler(d), r)
	statusOK(t, w, 503)
}

func TestListServers_NoIdentity(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := httptest.NewRequest("GET", "/v1/servers", nil) // no identity
	w := runHandler(listServersHandler(d), r)
	statusOK(t, w, 401)
}

func TestGetServer_HappyPath(t *testing.T) {
	d, _, _, _, _, _, _, _, reg, _ := testDeps(t)
	seedServer(t, reg, "srv1")
	r := newReq("GET", "/v1/servers/srv1", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(getServerHandler(d), r)
	statusOK(t, w, 200)
}

func TestGetServer_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("GET", "/v1/servers/ghost", nil)
	r = withChiURLParam(r, "id", "ghost")
	w := runHandler(getServerHandler(d), r)
	statusOK(t, w, 404)
}

func TestGetServer_NoRegistry(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.Registry = nil
	r := newReq("GET", "/v1/servers/srv1", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(getServerHandler(d), r)
	statusOK(t, w, 503)
}

func TestUpsertServer_PostCreate(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/v1/servers", validServerSpec("new-srv"))
	w := runHandler(upsertServerHandler(d, false), r)
	statusOK(t, w, 201)
}

func TestUpsertServer_PostBadJSON(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/v1/servers", nil)
	r.Body = httpReadCloser("not-json")
	w := runHandler(upsertServerHandler(d, false), r)
	statusOK(t, w, 400)
}

func TestUpsertServer_PutCreate404(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	spec := validServerSpec("ghost")
	r := newReq("PUT", "/v1/servers/ghost", spec)
	r = withChiURLParam(r, "id", "ghost")
	w := runHandler(upsertServerHandler(d, true), r)
	statusOK(t, w, 404)
}

func TestUpsertServer_PutIDMismatch(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	spec := validServerSpec("body-id")
	r := newReq("PUT", "/v1/servers/url-id", spec)
	r = withChiURLParam(r, "id", "url-id")
	w := runHandler(upsertServerHandler(d, true), r)
	statusOK(t, w, 400)
}

func TestUpsertServer_ValidationFailure(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	bad := &registry.ServerSpec{ID: "ok", Transport: "weird"}
	r := newReq("POST", "/v1/servers", bad)
	w := runHandler(upsertServerHandler(d, false), r)
	statusOK(t, w, 400)
}

func TestUpsertServer_NoRegistry(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.Registry = nil
	r := newReq("POST", "/v1/servers", validServerSpec("x"))
	w := runHandler(upsertServerHandler(d, false), r)
	statusOK(t, w, 503)
}

func TestDeleteServer_HappyPath(t *testing.T) {
	d, _, _, _, _, _, _, _, reg, _ := testDeps(t)
	seedServer(t, reg, "srv1")
	r := newReq("DELETE", "/v1/servers/srv1", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(deleteServerHandler(d), r)
	statusOK(t, w, 204)
}

func TestDeleteServer_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("DELETE", "/v1/servers/ghost", nil)
	r = withChiURLParam(r, "id", "ghost")
	w := runHandler(deleteServerHandler(d), r)
	statusOK(t, w, 404)
}

func TestDeleteServer_NoRegistry(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.Registry = nil
	r := newReq("DELETE", "/v1/servers/srv1", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(deleteServerHandler(d), r)
	statusOK(t, w, 503)
}

func TestReloadServer_HappyPath(t *testing.T) {
	d, _, _, _, _, _, _, _, reg, _ := testDeps(t)
	seedServer(t, reg, "srv1")
	r := newReq("POST", "/v1/servers/srv1/reload", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(reloadServerHandler(d), r)
	statusOK(t, w, 202)
}

func TestReloadServer_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/v1/servers/ghost/reload", nil)
	r = withChiURLParam(r, "id", "ghost")
	w := runHandler(reloadServerHandler(d), r)
	statusOK(t, w, 404)
}

func TestReloadServer_NoRegistry(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.Registry = nil
	r := newReq("POST", "/v1/servers/srv1/reload", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(reloadServerHandler(d), r)
	statusOK(t, w, 503)
}

func TestEnableServer_HappyPath(t *testing.T) {
	d, _, _, _, _, _, _, _, reg, _ := testDeps(t)
	seedServer(t, reg, "srv1")
	r := newReq("POST", "/v1/servers/srv1/enable", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(enableServerHandler(d, true), r)
	statusOK(t, w, 200)
}

func TestEnableServer_Disable(t *testing.T) {
	d, _, _, _, _, _, _, _, reg, _ := testDeps(t)
	seedServer(t, reg, "srv1")
	r := newReq("POST", "/v1/servers/srv1/disable", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(enableServerHandler(d, false), r)
	statusOK(t, w, 200)
}

func TestEnableServer_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("POST", "/v1/servers/ghost/enable", nil)
	r = withChiURLParam(r, "id", "ghost")
	w := runHandler(enableServerHandler(d, true), r)
	statusOK(t, w, 404)
}

func TestEnableServer_NoRegistry(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.Registry = nil
	r := newReq("POST", "/v1/servers/srv1/enable", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(enableServerHandler(d, true), r)
	statusOK(t, w, 503)
}

func TestListInstances_HappyPath(t *testing.T) {
	d, _, _, _, _, _, _, _, reg, _ := testDeps(t)
	seedServer(t, reg, "srv1")
	r := newReq("GET", "/v1/servers/srv1/instances", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(listInstancesHandler(d), r)
	statusOK(t, w, 200)
}

func TestListInstances_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	r := newReq("GET", "/v1/servers/ghost/instances", nil)
	r = withChiURLParam(r, "id", "ghost")
	w := runHandler(listInstancesHandler(d), r)
	statusOK(t, w, 404)
}

func TestListInstances_NoRegistry(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.Registry = nil
	r := newReq("GET", "/v1/servers/srv1/instances", nil)
	r = withChiURLParam(r, "id", "srv1")
	w := runHandler(listInstancesHandler(d), r)
	statusOK(t, w, 503)
}

// upsertServerHandler also exercises the body-id-fills-from-path branch
// when the path id is set and the body id is empty. Seed first since
// upsert-on-missing returns 404 in update mode.
func TestUpsertServer_FillsIDFromPath(t *testing.T) {
	spec := *validServerSpec("")
	spec.ID = ""
	d, _, _, _, _, _, _, _, reg, _ := testDeps(t)
	seedServer(t, reg, "auto")
	r := newReq("PUT", "/v1/servers/auto", spec)
	r = withChiURLParam(r, "id", "auto")
	w := runHandler(upsertServerHandler(d, true), r)
	statusOK(t, w, 200)
}
