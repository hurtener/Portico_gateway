package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func TestAgentProfileStore_RoundTrip(t *testing.T) {
	db := open(t)
	store := db.AgentProfiles()
	ctx := context.Background()

	profile := &ifaces.AgentProfile{
		TenantID:            "tenant-a",
		ID:                  "profile-1",
		Name:                "Test Profile",
		Description:         "A test profile",
		AllowedMCPServers:   []string{"github", "jira"},
		AllowedTools:        []string{"github.list_issues", "jira.create_ticket"},
		AllowedSkills:       []string{"skill-1", "skill-2"},
		AllowedModelAliases: []string{"gpt-4o", "claude-3-5"},
		Scopes:              []string{"mcp:call", "llm:invoke"},
		PolicyBundleRef:     "policy-123",
		ParentProfileID:     "parent-1",
		Enabled:             true,
	}

	if err := store.Put(ctx, profile); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := store.Get(ctx, "tenant-a", "profile-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.TenantID != profile.TenantID || got.ID != profile.ID || got.Name != profile.Name ||
		got.Description != profile.Description || got.PolicyBundleRef != profile.PolicyBundleRef ||
		got.ParentProfileID != profile.ParentProfileID || got.Enabled != profile.Enabled {
		t.Errorf("scalar fields mismatch: got %+v", got)
	}

	assertSliceEqual(t, "AllowedMCPServers", got.AllowedMCPServers, profile.AllowedMCPServers)
	assertSliceEqual(t, "AllowedTools", got.AllowedTools, profile.AllowedTools)
	assertSliceEqual(t, "AllowedSkills", got.AllowedSkills, profile.AllowedSkills)
	assertSliceEqual(t, "AllowedModelAliases", got.AllowedModelAliases, profile.AllowedModelAliases)
	assertSliceEqual(t, "Scopes", got.Scopes, profile.Scopes)

	if got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Errorf("timestamps not set: created=%q updated=%q", got.CreatedAt, got.UpdatedAt)
	}
}

func TestAgentProfileStore_Put_ReplacesAllowlists(t *testing.T) {
	db := open(t)
	store := db.AgentProfiles()
	ctx := context.Background()

	profile := &ifaces.AgentProfile{
		TenantID:            "tenant-a",
		ID:                  "profile-1",
		Name:                "Test Profile",
		AllowedMCPServers:   []string{"github"},
		AllowedTools:        []string{"github.list_issues"},
		AllowedSkills:       []string{"skill-1"},
		AllowedModelAliases: []string{"gpt-4o"},
		Scopes:              []string{"mcp:call"},
		Enabled:             true,
	}

	if err := store.Put(ctx, profile); err != nil {
		t.Fatalf("first put: %v", err)
	}

	// Update with different allowlists
	profile.AllowedMCPServers = []string{"jira", "slack"}
	profile.AllowedTools = []string{"jira.create_ticket"}
	profile.AllowedSkills = []string{"skill-2", "skill-3"}
	profile.AllowedModelAliases = []string{"claude-3-5"}

	if err := store.Put(ctx, profile); err != nil {
		t.Fatalf("second put: %v", err)
	}

	got, err := store.Get(ctx, "tenant-a", "profile-1")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}

	assertSliceEqual(t, "AllowedMCPServers", got.AllowedMCPServers, []string{"jira", "slack"})
	assertSliceEqual(t, "AllowedTools", got.AllowedTools, []string{"jira.create_ticket"})
	assertSliceEqual(t, "AllowedSkills", got.AllowedSkills, []string{"skill-2", "skill-3"})
	assertSliceEqual(t, "AllowedModelAliases", got.AllowedModelAliases, []string{"claude-3-5"})
}

func TestAgentProfileStore_List_TenantIsolated(t *testing.T) {
	db := open(t)
	store := db.AgentProfiles()
	ctx := context.Background()

	for _, p := range []*ifaces.AgentProfile{
		{TenantID: "tenant-a", ID: "p1", Name: "Profile A1", Enabled: true},
		{TenantID: "tenant-a", ID: "p2", Name: "Profile A2", Enabled: true},
		{TenantID: "tenant-b", ID: "p1", Name: "Profile B1", Enabled: true},
		{TenantID: "tenant-b", ID: "p2", Name: "Profile B2", Enabled: true},
	} {
		if err := store.Put(ctx, p); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	a, err := store.List(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("list tenant-a: %v", err)
	}
	if len(a) != 2 {
		t.Fatalf("tenant-a: want 2 profiles, got %d", len(a))
	}
	for _, p := range a {
		if p.TenantID != "tenant-a" {
			t.Errorf("cross-tenant leak in list: %+v", p)
		}
	}

	b, err := store.List(ctx, "tenant-b")
	if err != nil {
		t.Fatalf("list tenant-b: %v", err)
	}
	if len(b) != 2 {
		t.Fatalf("tenant-b: want 2 profiles, got %d", len(b))
	}
	for _, p := range b {
		if p.TenantID != "tenant-b" {
			t.Errorf("cross-tenant leak in list: %+v", p)
		}
	}
}

func TestAgentProfileStore_Get_NotFound(t *testing.T) {
	db := open(t)
	store := db.AgentProfiles()
	ctx := context.Background()

	_, err := store.Get(ctx, "tenant-a", "nonexistent")
	if !errors.Is(err, ifaces.ErrAgentProfileNotFound) {
		t.Fatalf("want ErrAgentProfileNotFound, got %v", err)
	}
}

func TestAgentProfileStore_Delete_CascadesAllowlists(t *testing.T) {
	db := open(t)
	store := db.AgentProfiles()
	ctx := context.Background()

	profile := &ifaces.AgentProfile{
		TenantID:            "tenant-a",
		ID:                  "profile-1",
		Name:                "Test Profile",
		AllowedMCPServers:   []string{"github", "jira"},
		AllowedTools:        []string{"github.list_issues"},
		AllowedSkills:       []string{"skill-1"},
		AllowedModelAliases: []string{"gpt-4o"},
		Enabled:             true,
	}

	if err := store.Put(ctx, profile); err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := store.Delete(ctx, "tenant-a", "profile-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Get should return not found
	if _, err := store.Get(ctx, "tenant-a", "profile-1"); !errors.Is(err, ifaces.ErrAgentProfileNotFound) {
		t.Fatalf("want ErrAgentProfileNotFound after delete, got %v", err)
	}

	// Direct count on join table should be 0
	var count int
	err := db.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM agent_profile_mcp_servers WHERE tenant_id = ? AND profile_id = ?
	`, "tenant-a", "profile-1").Scan(&count)
	if err != nil {
		t.Fatalf("count mcp_servers: %v", err)
	}
	if count != 0 {
		t.Errorf("mcp_servers not cascaded: count = %d", count)
	}
}

func TestAgentProfileStore_JWTBinding_ResolveAndIsolation(t *testing.T) {
	db := open(t)
	store := db.AgentProfiles()
	ctx := context.Background()

	profile := &ifaces.AgentProfile{
		TenantID:          "tenant-a",
		ID:                "profile-1",
		Name:              "Test Profile",
		AllowedMCPServers: []string{"github"},
		Enabled:           true,
	}

	if err := store.Put(ctx, profile); err != nil {
		t.Fatalf("put profile: %v", err)
	}

	// Bind JWT subject to profile
	if err := store.PutJWTBinding(ctx, "tenant-a", "user-123", "profile-1"); err != nil {
		t.Fatalf("put jwt binding: %v", err)
	}

	// Resolve should return the bound profile with allowlists
	got, err := store.ResolveJWTBinding(ctx, "tenant-a", "user-123")
	if err != nil {
		t.Fatalf("resolve jwt binding: %v", err)
	}
	if got.ID != "profile-1" || len(got.AllowedMCPServers) != 1 || got.AllowedMCPServers[0] != "github" {
		t.Errorf("resolve returned wrong profile: %+v", got)
	}

	// Different tenant with same jwt_sub should not find the binding
	_, err = store.ResolveJWTBinding(ctx, "tenant-b", "user-123")
	if !errors.Is(err, ifaces.ErrAgentProfileNotFound) {
		t.Fatalf("want ErrAgentProfileNotFound for cross-tenant resolve, got %v", err)
	}

	// Unbound subject should return not found
	_, err = store.ResolveJWTBinding(ctx, "tenant-a", "unknown-user")
	if !errors.Is(err, ifaces.ErrAgentProfileNotFound) {
		t.Fatalf("want ErrAgentProfileNotFound for unbound subject, got %v", err)
	}
}

func assertSliceEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: len(got)=%d len(want)=%d", label, len(got), len(want))
		return
	}
	// Order-insensitive compare via map
	wantMap := make(map[string]bool, len(want))
	for _, v := range want {
		wantMap[v] = true
	}
	for _, v := range got {
		if !wantMap[v] {
			t.Errorf("%s: got unexpected value %q", label, v)
		}
	}
	for _, v := range want {
		found := false
		for _, gv := range got {
			if gv == v {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: missing expected value %q", label, v)
		}
	}
}
