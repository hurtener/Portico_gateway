package api

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// stubAgentProfileStore implements ifaces.AgentProfileStore for testing.
type stubAgentProfileStore struct {
	mu       sync.Mutex
	profiles map[string]map[string]*ifaces.AgentProfile // tenantID -> profileID -> profile
}

func newStubAgentProfileStore() *stubAgentProfileStore {
	return &stubAgentProfileStore{profiles: map[string]map[string]*ifaces.AgentProfile{}}
}

func profileKey(tenantID, id string) string { return tenantID + "/" + id }

func (s *stubAgentProfileStore) List(_ context.Context, tenantID string) ([]*ifaces.AgentProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := []*ifaces.AgentProfile{}
	for _, p := range s.profiles[tenantID] {
		cp := *p
		rows = append(rows, &cp)
	}
	return rows, nil
}

func (s *stubAgentProfileStore) Get(_ context.Context, tenantID, id string) (*ifaces.AgentProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[tenantID][id]
	if !ok {
		return nil, ifaces.ErrAgentProfileNotFound
	}
	cp := *p
	return &cp, nil
}

func (s *stubAgentProfileStore) Put(_ context.Context, p *ifaces.AgentProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.profiles[p.TenantID]; !ok {
		s.profiles[p.TenantID] = map[string]*ifaces.AgentProfile{}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	cp := *p
	if cp.CreatedAt == "" {
		cp.CreatedAt = now
	}
	cp.UpdatedAt = now
	s.profiles[p.TenantID][p.ID] = &cp
	return nil
}

func (s *stubAgentProfileStore) Delete(_ context.Context, tenantID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.profiles[tenantID][id]; !ok {
		return ifaces.ErrAgentProfileNotFound
	}
	delete(s.profiles[tenantID], id)
	return nil
}

// JWT binding methods (minimal no-op implementations for interface compliance).
func (s *stubAgentProfileStore) PutJWTBinding(_ context.Context, tenantID, jwtSub, profileID string) error {
	return nil
}

func (s *stubAgentProfileStore) DeleteJWTBinding(_ context.Context, tenantID, jwtSub string) error {
	return nil
}

func (s *stubAgentProfileStore) ResolveJWTBinding(_ context.Context, tenantID, jwtSub string) (*ifaces.AgentProfile, error) {
	return nil, ifaces.ErrAgentProfileNotFound
}

func TestListAgentProfiles_Happy(t *testing.T) {
	store := newStubAgentProfileStore()
	// Seed two under t1, one under other tenant.
	now := time.Now().UTC().Format(time.RFC3339)
	store.profiles["t1"] = map[string]*ifaces.AgentProfile{
		"ap_1": {TenantID: "t1", ID: "ap_1", Name: "Profile One", Enabled: true, CreatedAt: now, UpdatedAt: now},
		"ap_2": {TenantID: "t1", ID: "ap_2", Name: "Profile Two", Enabled: true, CreatedAt: now, UpdatedAt: now},
	}
	store.profiles["other"] = map[string]*ifaces.AgentProfile{
		"ap_3": {TenantID: "other", ID: "ap_3", Name: "Other Profile", Enabled: true, CreatedAt: now, UpdatedAt: now},
	}

	d := Deps{AgentProfiles: store}
	w := runHandler(listAgentProfilesHandler(d), newReq("GET", "/api/agent-profiles", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var out []AgentProfileDTO
	decodeJSON(t, w, &out)
	if len(out) != 2 {
		t.Fatalf("expected 2 profiles for t1, got %d: %+v", len(out), out)
	}
	// Verify tenant isolation - only t1's profiles returned.
	for _, p := range out {
		if p.ID == "ap_3" {
			t.Fatalf("tenant isolation violated: other tenant's profile returned")
		}
	}
}

func TestCreateAgentProfile_GeneratesIDAnd201(t *testing.T) {
	store := newStubAgentProfileStore()
	d := Deps{AgentProfiles: store}
	body := map[string]any{"name": "New Profile", "description": "test desc"}
	w := runHandler(createAgentProfileHandler(d), newReq("POST", "/api/agent-profiles", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", w.Code, w.Body.String())
	}
	var out AgentProfileDTO
	decodeJSON(t, w, &out)
	if out.ID == "" {
		t.Fatal("expected generated ID, got empty")
	}
	if out.Name != "New Profile" {
		t.Fatalf("name mismatch: got %q", out.Name)
	}
	if out.Description != "test desc" {
		t.Fatalf("description mismatch: got %q", out.Description)
	}
	// Verify ID format starts with ap_
	if len(out.ID) < 4 || out.ID[:3] != "ap_" {
		t.Fatalf("ID does not start with 'ap_': %s", out.ID)
	}
}

func TestCreateAgentProfile_RequiresName(t *testing.T) {
	store := newStubAgentProfileStore()
	d := Deps{AgentProfiles: store}
	w := runHandler(createAgentProfileHandler(d), newReq("POST", "/api/agent-profiles", map[string]any{}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	decodeJSON(t, w, &body)
	if body["error"] != "invalid_request" {
		t.Fatalf("expected error code invalid_request, got %v", body["error"])
	}
}

func TestGetAgentProfile_NotFound(t *testing.T) {
	store := newStubAgentProfileStore()
	d := Deps{AgentProfiles: store}
	r := newReq("GET", "/api/agent-profiles/nope", nil)
	r = withChiURLParam(r, "id", "nope")
	w := runHandler(getAgentProfileHandler(d), r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	decodeJSON(t, w, &body)
	if body["error"] != "not_found" {
		t.Fatalf("expected error code not_found, got %v", body["error"])
	}
}

func TestUpdateAgentProfile_NotFound(t *testing.T) {
	store := newStubAgentProfileStore()
	d := Deps{AgentProfiles: store}
	body := map[string]any{"name": "Updated"}
	r := newReq("PUT", "/api/agent-profiles/nope", body)
	r = withChiURLParam(r, "id", "nope")
	w := runHandler(updateAgentProfileHandler(d), r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
	var bodyResp map[string]any
	decodeJSON(t, w, &bodyResp)
	if bodyResp["error"] != "not_found" {
		t.Fatalf("expected error code not_found, got %v", bodyResp["error"])
	}
}

func TestDeleteAgentProfile_204(t *testing.T) {
	store := newStubAgentProfileStore()
	now := time.Now().UTC().Format(time.RFC3339)
	store.profiles["t1"] = map[string]*ifaces.AgentProfile{
		"ap_1": {TenantID: "t1", ID: "ap_1", Name: "To Delete", Enabled: true, CreatedAt: now, UpdatedAt: now},
	}

	d := Deps{AgentProfiles: store}
	// DELETE
	rDel := newReq("DELETE", "/api/agent-profiles/ap_1", nil)
	rDel = withChiURLParam(rDel, "id", "ap_1")
	w := runHandler(deleteAgentProfileHandler(d), rDel)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204 (%s)", w.Code, w.Body.String())
	}
	// GET should now 404
	rGet := newReq("GET", "/api/agent-profiles/ap_1", nil)
	rGet = withChiURLParam(rGet, "id", "ap_1")
	w = runHandler(getAgentProfileHandler(d), rGet)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET after DELETE status = %d, want 404 (%s)", w.Code, w.Body.String())
	}
}

func TestAgentProfiles_NilStore503(t *testing.T) {
	w := runHandler(listAgentProfilesHandler(Deps{}), newReq("GET", "/api/agent-profiles", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	decodeJSON(t, w, &body)
	if body["error"] != "agent_profiles_not_configured" {
		t.Fatalf("expected error agent_profiles_not_configured, got %v", body["error"])
	}
}

func TestAgentProfiles_NonAdmin403(t *testing.T) {
	store := newStubAgentProfileStore()
	d := Deps{AgentProfiles: store}
	w := runHandler(listAgentProfilesHandler(d), newReq("GET", "/api/agent-profiles", nil, "llm:invoke"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for non-admin (%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	decodeJSON(t, w, &body)
	if body["error"] != "forbidden" {
		t.Fatalf("expected error forbidden, got %v", body["error"])
	}
}
