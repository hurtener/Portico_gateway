package api

import (
	"net/http"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type surfaceResp struct {
	ProfileID string   `json:"profile_id"`
	IsDefault bool     `json:"is_default"`
	Servers   []string `json:"servers"`
	Tools     []string `json:"tools"`
	Skills    []string `json:"skills"`
	Models    []string `json:"models"`
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestAgentProfileSurface_LiveRegistryIntersection proves acceptance #12: the
// surface intersects the profile's allowlist with the LIVE registry, so a
// server registered AFTER the profile was created (but matching the allowlist)
// appears immediately.
func TestAgentProfileSurface_LiveRegistryIntersection(t *testing.T) {
	d, _, _, _, _, _, _, _, reg, _ := testDeps(t)
	store := newStubAgentProfileStore()
	d.AgentProfiles = store
	store.profiles["t1"] = map[string]*ifaces.AgentProfile{
		"ap_1": {TenantID: "t1", ID: "ap_1", Name: "p", AllowedMCPServers: []string{"github", "jira"}, Enabled: true},
	}

	// Only github registered at first.
	seedServer(t, reg, "github")

	r := withChiURLParam(newReq("GET", "/api/agent-profiles/ap_1/surface", nil), "id", "ap_1")
	w := runHandler(agentProfileSurfaceHandler(d), r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var got surfaceResp
	decodeJSON(t, w, &got)
	if !contains(got.Servers, "github") {
		t.Fatalf("github should be in surface: %+v", got.Servers)
	}
	if contains(got.Servers, "jira") {
		t.Fatalf("jira not registered yet — must not appear: %+v", got.Servers)
	}
	if got.ProfileID != "ap_1" || got.IsDefault {
		t.Fatalf("surface meta wrong: %+v", got)
	}

	// Register jira AFTER the profile exists — it must appear immediately.
	seedServer(t, reg, "jira")
	r2 := withChiURLParam(newReq("GET", "/api/agent-profiles/ap_1/surface", nil), "id", "ap_1")
	w2 := runHandler(agentProfileSurfaceHandler(d), r2)
	var got2 surfaceResp
	decodeJSON(t, w2, &got2)
	if !contains(got2.Servers, "github") || !contains(got2.Servers, "jira") {
		t.Fatalf("server added after profile creation must appear immediately: %+v", got2.Servers)
	}
}

func TestAgentProfileSurface_NotFound(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.AgentProfiles = newStubAgentProfileStore()
	r := withChiURLParam(newReq("GET", "/api/agent-profiles/nope/surface", nil), "id", "nope")
	w := runHandler(agentProfileSurfaceHandler(d), r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
}

func TestAgentProfileSurface_StoreUnconfigured503(t *testing.T) {
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	d.AgentProfiles = nil
	r := withChiURLParam(newReq("GET", "/api/agent-profiles/x/surface", nil), "id", "x")
	w := runHandler(agentProfileSurfaceHandler(d), r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", w.Code, w.Body.String())
	}
}
