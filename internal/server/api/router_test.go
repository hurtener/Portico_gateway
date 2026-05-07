package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/apps"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
)

// Dispatcher is required for /v1/resources, /v1/prompts and /mcp routes.
// We construct one without aggregators wired so the handlers hit the 503
// path. That's enough to bump router.go coverage past the gate without
// dragging in the full southbound manager + protocol stack.

// router_test.go exercises NewRouter end-to-end with a synthesised dev-mode
// Deps. The intent is not to re-test every handler — the per-handler tests
// already cover behaviour. Here we drive enough request-paths through the
// chi mux + auth middleware to lift router.go's coverage above the gate.

func newRouterTestDeps(t *testing.T) Deps {
	t.Helper()
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.DevMode = true
	d.DevTenant = "t1"
	d.SkillSources = newStubSkillSources()
	d.AuthoredSkills = newStubAuthoredSkills()
	d.SkillValidator = &stubSkillValidator{}
	d.Approvals = newStubApprovalStore()
	d.Audit = newStubAuditStore()
	d.Apps = apps.New(apps.CSPConfig{})
	d.SnapshotBinder = mcpgw.NewSnapshotBinder(nil)
	d.Dispatcher = mcpgw.NewDispatcher(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	d.ApprovalFlow = NewApprovalFlowAdapter(nil)
	return d
}

func TestNewRouter_Healthz(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/healthz", nil)
	w := runHandler(h, r)
	statusOK(t, w, 200)
}

func TestNewRouter_Readyz(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/readyz", nil)
	w := runHandler(h, r)
	statusOK(t, w, 200)
}

func TestNewRouter_NotFound(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	// /v1/ prefixes are guaranteed-not-SPA. The Console UI mounts a
	// catch-all but pass-throughs the api prefixes to the chi 404 envelope.
	r := httptest.NewRequest("GET", "/v1/no-such-route", nil)
	w := runHandler(h, r)
	statusOK(t, w, 404)
}

func TestNewRouter_AuditEvents(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/v1/audit/events", nil)
	w := runHandler(h, r)
	statusOK(t, w, 200)
}

func TestNewRouter_ListServers(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/v1/servers", nil)
	w := runHandler(h, r)
	statusOK(t, w, 200)
}

func TestNewRouter_AdminTenants(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/v1/admin/tenants", nil)
	w := runHandler(h, r)
	statusOK(t, w, 200)
}

func TestNewRouter_SkillSources(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/api/skill-sources", nil)
	w := runHandler(h, r)
	statusOK(t, w, 200)
}

func TestNewRouter_AuthoredSkills(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/api/skills/authored", nil)
	w := runHandler(h, r)
	statusOK(t, w, 200)
}

func TestNewRouter_PolicyRules(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/api/policy/rules", nil)
	w := runHandler(h, r)
	statusOK(t, w, 200)
}

func TestNewRouter_AdminSecrets(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/api/admin/secrets", nil)
	w := runHandler(h, r)
	statusOK(t, w, 200)
}

func TestNewRouter_MethodNotAllowed(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("PATCH", "/healthz", nil)
	w := runHandler(h, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}

func TestNewRouter_Approvals(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/v1/approvals", nil)
	w := runHandler(h, r)
	statusOK(t, w, 200)
}

func TestNewRouter_ListApps(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/v1/apps", nil)
	w := runHandler(h, r)
	statusOK(t, w, 200)
}

func TestNewRouter_SessionSnapshot_NotFound(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/v1/sessions/no-session/snapshot", nil)
	w := runHandler(h, r)
	statusOK(t, w, 404)
}

// Dispatcher is wired but its Resources/Prompts aggregators are not, so
// these routes hit the 503 / "not configured" branch. That's sufficient
// to lift the package coverage past the gate.
func TestNewRouter_ListResources_503(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/v1/resources", nil)
	w := runHandler(h, r)
	statusOK(t, w, 503)
}

func TestNewRouter_ListPrompts_503(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/v1/prompts", nil)
	w := runHandler(h, r)
	statusOK(t, w, 503)
}

func TestNewRouter_ResourceTemplates_503(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/v1/resources/templates", nil)
	w := runHandler(h, r)
	statusOK(t, w, 503)
}

func TestNewRouter_GetPrompt_503(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("POST", "/v1/prompts/foo", nil)
	w := runHandler(h, r)
	statusOK(t, w, 503)
}

func TestNewRouter_ReadResource_503(t *testing.T) {
	d := newRouterTestDeps(t)
	h := NewRouter(d)
	r := httptest.NewRequest("GET", "/v1/resources/mcp+server%3A%2F%2Fexample", nil)
	w := runHandler(h, r)
	statusOK(t, w, 503)
}
