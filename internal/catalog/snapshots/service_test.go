package snapshots_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// inMemStore is a minimal snapshots.Store implementation for tests. It is
// deliberately separate from the SQLite-backed adapter — the goal is to
// exercise Service against the Store contract without requiring a DB.
type inMemStore struct {
	mu                  sync.Mutex
	snaps               map[string]*snapshots.Snapshot
	stampedSessions     map[string]string // sessionID -> snapshotID
	upsertedFingerprint []fpKey
}

type fpKey struct {
	TenantID   string
	ServerID   string
	Hash       string
	ToolsCount int
}

func newInMemStore() *inMemStore {
	return &inMemStore{
		snaps:           make(map[string]*snapshots.Snapshot),
		stampedSessions: make(map[string]string),
	}
}

func (s *inMemStore) Insert(_ context.Context, snap *snapshots.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Round-trip via JSON so the Store's later Get returns an independent
	// pointer (mirrors the SQLite adapter behaviour).
	body, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	var stored snapshots.Snapshot
	if err := json.Unmarshal(body, &stored); err != nil {
		return err
	}
	s.snaps[snap.ID] = &stored
	return nil
}

func (s *inMemStore) Get(_ context.Context, id string) (*snapshots.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.snaps[id]
	if !ok {
		return nil, snapshots.ErrNotFound
	}
	body, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	var out snapshots.Snapshot
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *inMemStore) List(_ context.Context, tenantID string, _ snapshots.ListQuery) ([]*snapshots.Snapshot, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*snapshots.Snapshot, 0)
	for _, snap := range s.snaps {
		if snap.TenantID == tenantID {
			cp := *snap
			out = append(out, &cp)
		}
	}
	return out, "", nil
}

func (s *inMemStore) StampSession(_ context.Context, sessionID, snapshotID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stampedSessions[sessionID] = snapshotID
	return nil
}

func (s *inMemStore) UpsertFingerprint(_ context.Context, tenantID, serverID, hash string, toolsCount int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upsertedFingerprint = append(s.upsertedFingerprint, fpKey{
		TenantID:   tenantID,
		ServerID:   serverID,
		Hash:       hash,
		ToolsCount: toolsCount,
	})
	return nil
}

func (s *inMemStore) LatestFingerprint(_ context.Context, _ string, _ string) (string, error) {
	return "", nil
}

func (s *inMemStore) ActiveSessions(_ context.Context, _ time.Time) ([]snapshots.ActiveSession, error) {
	return nil, nil
}

// Test helpers: read-side accessors for inMemStore.

func (s *inMemStore) snapshotCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.snaps)
}

func (s *inMemStore) stampFor(sessionID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.stampedSessions[sessionID]
	return v, ok
}

func (s *inMemStore) fingerprintCalls() []fpKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]fpKey, len(s.upsertedFingerprint))
	copy(out, s.upsertedFingerprint)
	return out
}

// fakeProbe is a CatalogProbe that returns a deterministic fixture and
// records the tenantIDs it was queried with.
type fakeProbe struct {
	mu               sync.Mutex
	queriedTenants   []string
	servers          []snapshots.ServerInfo
	tools            []snapshots.NamespacedTool
	resources        []snapshots.NamespacedResource
	prompts          []snapshots.NamespacedPrompt
	skills           []snapshots.SkillInfo
	credentials      []snapshots.CredentialInfo
	policies         snapshots.PoliciesInfo
	resolveOverrides map[string]toolPolicy
	defaultPolicy    toolPolicy
}

type toolPolicy struct {
	risk             string
	requiresApproval bool
	skillID          string
}

func (p *fakeProbe) recordTenant(tenantID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queriedTenants = append(p.queriedTenants, tenantID)
}

func (p *fakeProbe) tenants() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.queriedTenants))
	copy(out, p.queriedTenants)
	return out
}

func (p *fakeProbe) ListTools(_ context.Context, tenantID, _ string) ([]snapshots.NamespacedTool, error) {
	p.recordTenant(tenantID)
	out := make([]snapshots.NamespacedTool, len(p.tools))
	copy(out, p.tools)
	return out, nil
}

func (p *fakeProbe) ListResources(_ context.Context, tenantID, _ string) ([]snapshots.NamespacedResource, error) {
	p.recordTenant(tenantID)
	out := make([]snapshots.NamespacedResource, len(p.resources))
	copy(out, p.resources)
	return out, nil
}

func (p *fakeProbe) ListPrompts(_ context.Context, tenantID, _ string) ([]snapshots.NamespacedPrompt, error) {
	p.recordTenant(tenantID)
	out := make([]snapshots.NamespacedPrompt, len(p.prompts))
	copy(out, p.prompts)
	return out, nil
}

func (p *fakeProbe) ServerInfos(_ context.Context, tenantID string) ([]snapshots.ServerInfo, error) {
	p.recordTenant(tenantID)
	out := make([]snapshots.ServerInfo, len(p.servers))
	copy(out, p.servers)
	return out, nil
}

func (p *fakeProbe) SkillInfos(_ context.Context, tenantID, _ string) ([]snapshots.SkillInfo, error) {
	p.recordTenant(tenantID)
	out := make([]snapshots.SkillInfo, len(p.skills))
	copy(out, p.skills)
	return out, nil
}

func (p *fakeProbe) CredentialInfos(_ context.Context, tenantID string) ([]snapshots.CredentialInfo, error) {
	p.recordTenant(tenantID)
	out := make([]snapshots.CredentialInfo, len(p.credentials))
	copy(out, p.credentials)
	return out, nil
}

func (p *fakeProbe) PoliciesInfo(_ context.Context, tenantID string) snapshots.PoliciesInfo {
	p.recordTenant(tenantID)
	return p.policies
}

func (p *fakeProbe) ResolveToolPolicy(_ context.Context, tenantID, _, qualifiedName string) (string, bool, string) {
	p.recordTenant(tenantID)
	if pol, ok := p.resolveOverrides[qualifiedName]; ok {
		return pol.risk, pol.requiresApproval, pol.skillID
	}
	return p.defaultPolicy.risk, p.defaultPolicy.requiresApproval, p.defaultPolicy.skillID
}

func newFakeProbe() *fakeProbe {
	return &fakeProbe{
		servers: []snapshots.ServerInfo{
			{ID: "github", Transport: "http", Health: "ok"},
			{ID: "linear", Transport: "http", Health: "ok"},
		},
		tools: []snapshots.NamespacedTool{
			{
				NamespacedName: "github.create_issue",
				ServerID:       "github",
				Tool: protocol.Tool{
					Name:        "create_issue",
					Description: "Create an issue",
					InputSchema: json.RawMessage(`{"type":"object"}`),
				},
			},
			{
				NamespacedName: "github.delete_repo",
				ServerID:       "github",
				Tool: protocol.Tool{
					Name:        "delete_repo",
					Description: "Delete a repository",
					InputSchema: json.RawMessage(`{"type":"object"}`),
				},
			},
			{
				NamespacedName: "linear.list_issues",
				ServerID:       "linear",
				Tool: protocol.Tool{
					Name:        "list_issues",
					Description: "List Linear issues",
					InputSchema: json.RawMessage(`{"type":"object"}`),
				},
			},
		},
		resources: []snapshots.NamespacedResource{
			{URI: "github://repo/x", UpstreamURI: "x", ServerID: "github"},
		},
		prompts: []snapshots.NamespacedPrompt{
			{NamespacedName: "github.summarise", ServerID: "github"},
		},
		skills: []snapshots.SkillInfo{
			{ID: "skill_a", Version: "1.0.0", EnabledForSession: true},
		},
		credentials: []snapshots.CredentialInfo{
			{ServerID: "github", Strategy: "oauth", SecretRefs: []string{"github-token"}},
			{ServerID: "linear", Strategy: "header", SecretRefs: []string{"linear-key"}},
		},
		policies: snapshots.PoliciesInfo{DefaultRiskClass: "medium"},
		resolveOverrides: map[string]toolPolicy{
			"github.delete_repo": {risk: "high", requiresApproval: true},
		},
		defaultPolicy: toolPolicy{risk: "low", requiresApproval: false},
	}
}

func TestService_Create_DeterministicOverallHash(t *testing.T) {
	t.Parallel()
	store := newInMemStore()
	probe := newFakeProbe()
	svc := snapshots.NewService(store, probe, audit.NopEmitter{}, nil)

	ctx := context.Background()
	a, err := svc.Create(ctx, "acme", "sess_1")
	if err != nil {
		t.Fatalf("Create #1: %v", err)
	}
	b, err := svc.Create(ctx, "acme", "sess_1")
	if err != nil {
		t.Fatalf("Create #2: %v", err)
	}
	if a.OverallHash == "" {
		t.Fatal("OverallHash empty")
	}
	if a.OverallHash != b.OverallHash {
		t.Errorf("OverallHash should be stable across creates: %s vs %s", a.OverallHash, b.OverallHash)
	}
	if a.ID == b.ID {
		t.Errorf("Snapshot IDs should differ: %s == %s", a.ID, b.ID)
	}
}

func TestService_Create_DifferentTenant_DifferentSnapshot(t *testing.T) {
	t.Parallel()
	store := newInMemStore()
	probe := newFakeProbe()
	svc := snapshots.NewService(store, probe, audit.NopEmitter{}, nil)

	ctx := context.Background()
	a, err := svc.Create(ctx, "acme", "sess_a")
	if err != nil {
		t.Fatalf("Create acme: %v", err)
	}
	b, err := svc.Create(ctx, "beta", "sess_b")
	if err != nil {
		t.Fatalf("Create beta: %v", err)
	}
	if a.TenantID == b.TenantID {
		t.Error("snapshots should record distinct tenants")
	}
	if a.ID == b.ID {
		t.Errorf("snapshot ids should differ: %s == %s", a.ID, b.ID)
	}
	if got := store.snapshotCount(); got != 2 {
		t.Errorf("expected 2 snapshots stored, got %d", got)
	}
}

func TestService_Create_StampsSession(t *testing.T) {
	t.Parallel()
	store := newInMemStore()
	probe := newFakeProbe()
	svc := snapshots.NewService(store, probe, audit.NopEmitter{}, nil)

	snap, err := svc.Create(context.Background(), "acme", "sess_42")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, ok := store.stampFor("sess_42")
	if !ok {
		t.Fatal("expected session sess_42 to be stamped")
	}
	if got != snap.ID {
		t.Errorf("stamp = %s, want snapshot id %s", got, snap.ID)
	}
}

func TestService_Create_PerServerFingerprintsRecorded(t *testing.T) {
	t.Parallel()
	store := newInMemStore()
	probe := newFakeProbe()
	svc := snapshots.NewService(store, probe, audit.NopEmitter{}, nil)

	if _, err := svc.Create(context.Background(), "acme", "sess_1"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	calls := store.fingerprintCalls()
	if len(calls) != len(probe.servers) {
		t.Fatalf("expected %d UpsertFingerprint calls, got %d", len(probe.servers), len(calls))
	}
	seen := make(map[string]bool)
	for _, c := range calls {
		if c.TenantID != "acme" {
			t.Errorf("fingerprint call has wrong tenant %q", c.TenantID)
		}
		if c.Hash == "" {
			t.Errorf("fingerprint call recorded empty hash for server %s", c.ServerID)
		}
		seen[c.ServerID] = true
	}
	for _, sv := range probe.servers {
		if !seen[sv.ID] {
			t.Errorf("expected fingerprint upsert for server %s", sv.ID)
		}
	}
}

func TestService_Diff_AcrossSnapshots(t *testing.T) {
	t.Parallel()
	store := newInMemStore()
	probe := newFakeProbe()
	svc := snapshots.NewService(store, probe, audit.NopEmitter{}, nil)
	ctx := context.Background()

	a, err := svc.Create(ctx, "acme", "sess_1")
	if err != nil {
		t.Fatalf("Create A: %v", err)
	}

	// Mutate the probe: drop a tool, add a tool, change a description.
	probe.mu.Lock()
	probe.tools = []snapshots.NamespacedTool{
		{
			NamespacedName: "github.create_issue",
			ServerID:       "github",
			Tool: protocol.Tool{
				Name:        "create_issue",
				Description: "Create an issue (renamed)",
				InputSchema: json.RawMessage(`{"type":"object"}`),
			},
		},
		{
			NamespacedName: "linear.list_issues",
			ServerID:       "linear",
			Tool: protocol.Tool{
				Name:        "list_issues",
				Description: "List Linear issues",
				InputSchema: json.RawMessage(`{"type":"object"}`),
			},
		},
		{
			NamespacedName: "linear.create_issue",
			ServerID:       "linear",
			Tool: protocol.Tool{
				Name:        "create_issue",
				Description: "Create a Linear issue",
				InputSchema: json.RawMessage(`{"type":"object"}`),
			},
		},
	}
	probe.mu.Unlock()

	b, err := svc.Create(ctx, "acme", "sess_1")
	if err != nil {
		t.Fatalf("Create B: %v", err)
	}

	d, err := svc.Diff(ctx, a.ID, b.ID)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if d.IsEmpty() {
		t.Fatal("expected non-empty diff")
	}
	if !equalStrings(d.Tools.Added, []string{"linear.create_issue"}) {
		t.Errorf("Added = %v, want [linear.create_issue]", d.Tools.Added)
	}
	if !equalStrings(d.Tools.Removed, []string{"github.delete_repo"}) {
		t.Errorf("Removed = %v, want [github.delete_repo]", d.Tools.Removed)
	}
	if len(d.Tools.Modified) != 1 || d.Tools.Modified[0].Name != "github.create_issue" {
		t.Errorf("Modified = %+v, want one entry for github.create_issue", d.Tools.Modified)
	}
}

func TestService_Get_NotFound(t *testing.T) {
	t.Parallel()
	store := newInMemStore()
	probe := newFakeProbe()
	svc := snapshots.NewService(store, probe, audit.NopEmitter{}, nil)

	_, err := svc.Get(context.Background(), "snap_does_not_exist")
	if err == nil {
		t.Fatal("expected error for missing snapshot")
	}
	if !errors.Is(err, snapshots.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestService_Create_PerTenantIsolation(t *testing.T) {
	t.Parallel()
	store := newInMemStore()
	probe := newFakeProbe()
	svc := snapshots.NewService(store, probe, audit.NopEmitter{}, nil)

	if _, err := svc.Create(context.Background(), "acme", "sess_1"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, tid := range probe.tenants() {
		if tid != "acme" {
			t.Errorf("probe was queried with tenant %q; only \"acme\" expected", tid)
		}
	}

	// Second create for a different tenant: probe must not be queried for
	// "acme" again from this call. Reset the tracker first.
	probe.mu.Lock()
	probe.queriedTenants = nil
	probe.mu.Unlock()

	if _, err := svc.Create(context.Background(), "beta", "sess_2"); err != nil {
		t.Fatalf("Create beta: %v", err)
	}
	for _, tid := range probe.tenants() {
		if tid != "beta" {
			t.Errorf("probe was queried with tenant %q; only \"beta\" expected", tid)
		}
	}
}
