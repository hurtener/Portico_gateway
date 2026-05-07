package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/auth/scope"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/policy"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/secrets"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// ---- request helpers --------------------------------------------------

// newReq builds an *http.Request with body marshalled JSON when non-nil and
// an injected Identity carrying tenant t1, user "tester", and the supplied
// scopes (default: admin). Used everywhere the auth middleware would
// normally seed the context.
func newReq(method, path string, body any, scopes ...string) *http.Request {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, rdr)
	id := tenant.Identity{TenantID: "t1", UserID: "tester"}
	if len(scopes) == 0 {
		id.Scopes = []string{"admin"}
	} else {
		id.Scopes = append([]string(nil), scopes...)
	}
	r = r.WithContext(tenant.With(r.Context(), id))
	return r
}

// withChiURLParam injects a chi URL parameter into the request context. We
// use this when calling a handler directly (without mounting a chi router),
// because handlers read URL params via chi.URLParam. Repeated calls
// preserve previously-added params.
func withChiURLParam(r *http.Request, key, value string) *http.Request {
	rctx, ok := r.Context().Value(chi.RouteCtxKey).(*chi.Context)
	if !ok || rctx == nil {
		rctx = chi.NewRouteContext()
	}
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// decodeJSON unmarshals the response body into v.
func decodeJSON(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response: %v: body=%s", err, w.Body.String())
	}
}

// testDeps assembles a Deps with usable in-memory stubs for the relevant
// Phase 9 handlers. Individual tests overwrite or nil out fields they want
// to suppress.
func testDeps(t *testing.T) (Deps, *audit.SliceEmitter, *stubTenantStore, *stubEntityActivityStore, *stubVaultManager, *stubVaultReveal, *stubServerRuntimeStore, *stubPolicyRulesController, *registry.Registry, *memRegistryStore) {
	t.Helper()
	emitter := &audit.SliceEmitter{}
	tenants := newStubTenantStore()
	activity := newStubEntityActivityStore()
	vault := newStubVaultManager()
	reveal := newStubVaultReveal(vault)
	runtime := newStubServerRuntimeStore()
	rules := newStubPolicyRulesController()
	regStore := newMemRegistryStore()
	reg := registry.New(regStore, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d := Deps{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Tenants:        tenants,
		Audit:          nil,
		Registry:       reg,
		AuditEmitter:   emitter,
		EntityActivity: activity,
		PolicyRules:    rules,
		ServerRuntime:  runtime,
		Vault:          vault,
		VaultReveal:    reveal,
	}
	return d, emitter, tenants, activity, vault, reveal, runtime, rules, reg, regStore
}

// adminAuth wraps a handler with a fake auth middleware that injects the
// supplied identity, then optionally enforces the admin scope (mirroring
// the production router for tenants/secrets endpoints).
func adminAuth(id tenant.Identity, h http.Handler, requireAdmin bool) http.Handler {
	mw := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(tenant.With(r.Context(), id))
		if requireAdmin {
			scope.Require("admin")(h).ServeHTTP(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
	return mw
}

// ---- TenantStore stub -------------------------------------------------

type stubTenantStore struct {
	mu      sync.Mutex
	tenants map[string]*ifaces.Tenant
	failGet bool
}

func newStubTenantStore() *stubTenantStore {
	return &stubTenantStore{tenants: map[string]*ifaces.Tenant{}}
}

func (s *stubTenantStore) Get(_ context.Context, id string) (*ifaces.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failGet {
		return nil, errors.New("boom")
	}
	t, ok := s.tenants[id]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (s *stubTenantStore) List(_ context.Context) ([]*ifaces.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*ifaces.Tenant, 0, len(s.tenants))
	for _, v := range s.tenants {
		cp := *v
		out = append(out, &cp)
	}
	return out, nil
}

func (s *stubTenantStore) Upsert(_ context.Context, t *ifaces.Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *t
	s.tenants[t.ID] = &cp
	return nil
}

func (s *stubTenantStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[id]; !ok {
		return ifaces.ErrNotFound
	}
	delete(s.tenants, id)
	return nil
}

// ---- EntityActivityStore stub ----------------------------------------

type stubEntityActivityStore struct {
	mu   sync.Mutex
	rows []*ifaces.EntityActivityRecord
}

func newStubEntityActivityStore() *stubEntityActivityStore {
	return &stubEntityActivityStore{}
}

func (s *stubEntityActivityStore) Append(_ context.Context, r *ifaces.EntityActivityRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *r
	s.rows = append(s.rows, &cp)
	return nil
}

func (s *stubEntityActivityStore) List(_ context.Context, tenantID, kind, id string, limit int) ([]*ifaces.EntityActivityRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*ifaces.EntityActivityRecord{}
	for i := len(s.rows) - 1; i >= 0; i-- {
		row := s.rows[i]
		if row.TenantID != tenantID {
			continue
		}
		if kind != "" && row.EntityKind != kind {
			continue
		}
		if id != "" && row.EntityID != id {
			continue
		}
		cp := *row
		out = append(out, &cp)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// rowsCopy returns a snapshot of every row written.
func (s *stubEntityActivityStore) rowsCopy() []*ifaces.EntityActivityRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*ifaces.EntityActivityRecord, len(s.rows))
	for i, r := range s.rows {
		cp := *r
		out[i] = &cp
	}
	return out
}

// ---- VaultManager stub -----------------------------------------------

type stubVaultManager struct {
	mu      sync.Mutex
	entries map[string]string // tenant/name → value
	failPut bool
}

func newStubVaultManager() *stubVaultManager {
	return &stubVaultManager{entries: map[string]string{}}
}

func vaultKey(tenant, name string) string { return tenant + "/" + name }

func (v *stubVaultManager) Get(_ context.Context, tenantID, name string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	val, ok := v.entries[vaultKey(tenantID, name)]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return val, nil
}

func (v *stubVaultManager) Put(_ context.Context, tenantID, name, value string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.failPut {
		return errors.New("put failed")
	}
	v.entries[vaultKey(tenantID, name)] = value
	return nil
}

func (v *stubVaultManager) Delete(_ context.Context, tenantID, name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.entries[vaultKey(tenantID, name)]; !ok {
		return secrets.ErrNotFound
	}
	delete(v.entries, vaultKey(tenantID, name))
	return nil
}

func (v *stubVaultManager) List(_ context.Context, tenantID string) ([]string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	prefix := tenantID + "/"
	out := []string{}
	for k := range v.entries {
		if strings.HasPrefix(k, prefix) {
			out = append(out, strings.TrimPrefix(k, prefix))
		}
	}
	return out, nil
}

// ---- VaultRevealManager stub ----------------------------------------

type stubVaultReveal struct {
	mu     sync.Mutex
	tokens map[string]revealEntry
	vault  *stubVaultManager
	clock  func() time.Time
}

type revealEntry struct {
	tenant    string
	name      string
	actor     string
	expiresAt time.Time
}

func newStubVaultReveal(v *stubVaultManager) *stubVaultReveal {
	return &stubVaultReveal{
		tokens: map[string]revealEntry{},
		vault:  v,
		clock:  time.Now,
	}
}

func (s *stubVaultReveal) IssueRevealToken(ctx context.Context, tenantID, name, actorID string) (secrets.RevealToken, error) {
	if _, err := s.vault.Get(ctx, tenantID, name); err != nil {
		return secrets.RevealToken{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tok := "tok-" + tenantID + "-" + name
	exp := s.clock().Add(60 * time.Second)
	s.tokens[tok] = revealEntry{tenant: tenantID, name: name, actor: actorID, expiresAt: exp}
	return secrets.RevealToken{Token: tok, ExpiresAt: exp}, nil
}

func (s *stubVaultReveal) ConsumeReveal(ctx context.Context, token string) (string, string, string, string, error) {
	s.mu.Lock()
	entry, ok := s.tokens[token]
	if ok {
		delete(s.tokens, token)
	}
	s.mu.Unlock()
	if !ok {
		return "", "", "", "", errors.New("unknown token")
	}
	pt, err := s.vault.Get(ctx, entry.tenant, entry.name)
	if err != nil {
		return "", "", "", "", err
	}
	return pt, entry.tenant, entry.name, entry.actor, nil
}

// ---- ServerRuntimeStore stub ----------------------------------------

type stubServerRuntimeStore struct {
	mu       sync.Mutex
	records  map[string]*ifaces.ServerRuntimeRecord
	restarts []struct {
		tenant, server, reason string
		at                     time.Time
	}
}

func newStubServerRuntimeStore() *stubServerRuntimeStore {
	return &stubServerRuntimeStore{records: map[string]*ifaces.ServerRuntimeRecord{}}
}

func runtimeKey(tenant, server string) string { return tenant + "/" + server }

func (s *stubServerRuntimeStore) Get(_ context.Context, tenantID, serverID string) (*ifaces.ServerRuntimeRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[runtimeKey(tenantID, serverID)]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (s *stubServerRuntimeStore) Upsert(_ context.Context, r *ifaces.ServerRuntimeRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *r
	s.records[runtimeKey(r.TenantID, r.ServerID)] = &cp
	return nil
}

func (s *stubServerRuntimeStore) Delete(_ context.Context, tenantID, serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[runtimeKey(tenantID, serverID)]; !ok {
		return ifaces.ErrNotFound
	}
	delete(s.records, runtimeKey(tenantID, serverID))
	return nil
}

func (s *stubServerRuntimeStore) List(_ context.Context, tenantID string) ([]*ifaces.ServerRuntimeRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*ifaces.ServerRuntimeRecord{}
	for _, r := range s.records {
		if r.TenantID == tenantID {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *stubServerRuntimeStore) RecordRestart(_ context.Context, tenantID, serverID, reason string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restarts = append(s.restarts, struct {
		tenant, server, reason string
		at                     time.Time
	}{tenantID, serverID, reason, at})
	if r, ok := s.records[runtimeKey(tenantID, serverID)]; ok {
		r.LastRestartAt = at
		r.LastRestartReason = reason
	}
	return nil
}

// ---- PolicyRulesController stub --------------------------------------

type stubPolicyRulesController struct {
	mu    sync.Mutex
	rules map[string]map[string]policy.Rule // tenant → ruleID → rule
}

func newStubPolicyRulesController() *stubPolicyRulesController {
	return &stubPolicyRulesController{rules: map[string]map[string]policy.Rule{}}
}

func (s *stubPolicyRulesController) List(_ context.Context, tenantID string) (policy.RuleSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.rules[tenantID]
	if !ok {
		return policy.RuleSet{}, nil
	}
	out := make([]policy.Rule, 0, len(t))
	for _, r := range t {
		out = append(out, r)
	}
	return policy.RuleSet{Rules: out}, nil
}

func (s *stubPolicyRulesController) Get(_ context.Context, tenantID, ruleID string) (policy.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.rules[tenantID]
	if !ok {
		return policy.Rule{}, ifaces.ErrNotFound
	}
	r, ok := t[ruleID]
	if !ok {
		return policy.Rule{}, ifaces.ErrNotFound
	}
	return r, nil
}

func (s *stubPolicyRulesController) Upsert(_ context.Context, tenantID string, r policy.Rule) (policy.Rule, error) {
	if err := policy.Validate(r); err != nil {
		return policy.Rule{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rules[tenantID]; !ok {
		s.rules[tenantID] = map[string]policy.Rule{}
	}
	s.rules[tenantID][r.ID] = r
	return r, nil
}

func (s *stubPolicyRulesController) Delete(_ context.Context, tenantID, ruleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.rules[tenantID]
	if !ok {
		return ifaces.ErrNotFound
	}
	if _, ok := t[ruleID]; !ok {
		return ifaces.ErrNotFound
	}
	delete(t, ruleID)
	return nil
}

func (s *stubPolicyRulesController) ReplaceAll(_ context.Context, tenantID string, set policy.RuleSet) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := make(map[string]policy.Rule, len(set.Rules))
	for _, r := range set.Rules {
		t[r.ID] = r
	}
	s.rules[tenantID] = t
	return nil
}

// ---- RegistryStore stub (in-memory) ----------------------------------

type memRegistryStore struct {
	mu        sync.Mutex
	servers   map[string]*ifaces.ServerRecord
	instances map[string][]*ifaces.InstanceRecord
}

func newMemRegistryStore() *memRegistryStore {
	return &memRegistryStore{
		servers:   map[string]*ifaces.ServerRecord{},
		instances: map[string][]*ifaces.InstanceRecord{},
	}
}

func srvKey(tenantID, id string) string { return tenantID + "/" + id }

func (m *memRegistryStore) UpsertServer(_ context.Context, r *ifaces.ServerRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *r
	m.servers[srvKey(r.TenantID, r.ID)] = &cp
	return nil
}

func (m *memRegistryStore) GetServer(_ context.Context, tenantID, id string) (*ifaces.ServerRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.servers[srvKey(tenantID, id)]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (m *memRegistryStore) ListServers(_ context.Context, tenantID string) ([]*ifaces.ServerRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*ifaces.ServerRecord{}
	for _, r := range m.servers {
		if r.TenantID == tenantID {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *memRegistryStore) DeleteServer(_ context.Context, tenantID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.servers[srvKey(tenantID, id)]; !ok {
		return ifaces.ErrNotFound
	}
	delete(m.servers, srvKey(tenantID, id))
	return nil
}

func (m *memRegistryStore) UpdateServerStatus(_ context.Context, tenantID, id, status, detail string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.servers[srvKey(tenantID, id)]
	if !ok {
		return ifaces.ErrNotFound
	}
	r.Status = status
	r.StatusDetail = detail
	return nil
}

func (m *memRegistryStore) UpsertInstance(_ context.Context, _ *ifaces.InstanceRecord) error {
	return nil
}
func (m *memRegistryStore) DeleteInstance(_ context.Context, _, _ string) error { return nil }
func (m *memRegistryStore) ListInstances(_ context.Context, _, _ string) ([]*ifaces.InstanceRecord, error) {
	return nil, nil
}

// ---- spec helpers ----------------------------------------------------

// validServerSpec returns a registry.ServerSpec that passes Validate. id is
// the server id; the tests use this to seed the registry.
func validServerSpec(id string) *registry.ServerSpec {
	enabled := true
	return &registry.ServerSpec{
		ID:        id,
		Transport: "stdio",
		Stdio:     &registry.StdioSpec{Command: "/bin/true"},
		Enabled:   &enabled,
	}
}

// seedServer registers a server in the supplied registry under tenant t1.
func seedServer(t *testing.T, reg *registry.Registry, id string) {
	t.Helper()
	if _, err := reg.Apply(context.Background(), "t1", registry.Mutation{
		Op:     registry.MutOpCreate,
		Server: validServerSpec(id),
	}); err != nil {
		t.Fatalf("seed server: %v", err)
	}
}

// hasEvent reports whether the slice emitter saw an event of the given
// type. Used in audit assertions.
func hasEvent(em *audit.SliceEmitter, typ string) bool {
	for _, e := range em.Events() {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// statusOK fails the test if the recorder did not report the expected
// status. Returns the body bytes for follow-up parsing.
func statusOK(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status: want %d got %d, body=%s", want, w.Code, w.Body.String())
	}
}

// readErrorCode returns the "error" field from a JSON error body or an
// empty string when not present.
func readErrorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		return ""
	}
	if s, ok := body["error"].(string); ok {
		return s
	}
	return ""
}

// runHandler invokes h against a fresh recorder.
func runHandler(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// httpReadCloser wraps a string in an io.ReadCloser. Used by tests that
// need to send malformed bytes to a handler.
func httpReadCloser(s string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(s))
}

// activityRecordForServer returns a minimal EntityActivityRecord scoped to
// (tenant=t1, kind=server, id=<srvID>). Used to seed the entity_activity
// stub so the activity handler tests have rows to render.
func activityRecordForServer(srvID string) ifaces.EntityActivityRecord {
	return ifaces.EntityActivityRecord{
		TenantID:    "t1",
		EntityKind:  "server",
		EntityID:    srvID,
		EventID:     "ev-" + srvID,
		OccurredAt:  time.Now().UTC(),
		ActorUserID: "tester",
		Summary:     "server.touched",
	}
}

// activityRecordForSecret returns a minimal EntityActivityRecord scoped to
// (tenant=t1, kind=secret, id=<name>).
func activityRecordForSecret(name string) ifaces.EntityActivityRecord {
	return ifaces.EntityActivityRecord{
		TenantID:    "t1",
		EntityKind:  "secret",
		EntityID:    name,
		EventID:     "ev-" + name,
		OccurredAt:  time.Now().UTC(),
		ActorUserID: "tester",
		Summary:     "secret.touched",
	}
}

// stubSkillValidator returns no violations unless body equals "bad".
type stubSkillValidator struct{}

func (s *stubSkillValidator) Validate(body []byte) []ValidatorViolation {
	if string(body) == "bad" {
		return []ValidatorViolation{{Reason: "bad", Kind: "schema"}}
	}
	return nil
}

// ---- SkillSourcesController stub -------------------------------------

type stubSkillSources struct {
	mu      sync.Mutex
	records map[string]map[string]*ifaces.SkillSourceRecord // tenant → name → rec
}

func newStubSkillSources() *stubSkillSources {
	return &stubSkillSources{records: map[string]map[string]*ifaces.SkillSourceRecord{}}
}

func (s *stubSkillSources) List(_ context.Context, tenantID string) ([]*ifaces.SkillSourceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := []*ifaces.SkillSourceRecord{}
	for _, r := range s.records[tenantID] {
		cp := *r
		rows = append(rows, &cp)
	}
	return rows, nil
}

func (s *stubSkillSources) Get(_ context.Context, tenantID, name string) (*ifaces.SkillSourceRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[tenantID][name]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	cp := *rec
	return &cp, nil
}

func (s *stubSkillSources) Upsert(_ context.Context, r *ifaces.SkillSourceRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[r.TenantID]; !ok {
		s.records[r.TenantID] = map[string]*ifaces.SkillSourceRecord{}
	}
	cp := *r
	cp.UpdatedAt = time.Now().UTC()
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = cp.UpdatedAt
	}
	s.records[r.TenantID][r.Name] = &cp
	return nil
}

func (s *stubSkillSources) Delete(_ context.Context, tenantID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[tenantID][name]; !ok {
		return ifaces.ErrNotFound
	}
	delete(s.records[tenantID], name)
	return nil
}

func (s *stubSkillSources) Refresh(_ context.Context, tenantID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[tenantID][name]; !ok {
		return ifaces.ErrNotFound
	}
	return nil
}

func (s *stubSkillSources) ListPacks(_ context.Context, tenantID, name string) ([]SourcePack, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[tenantID][name]; !ok {
		return nil, ifaces.ErrNotFound
	}
	return []SourcePack{{ID: "p1", Version: "1.0.0"}}, nil
}

// ---- ApprovalStore stub ---------------------------------------------

type stubApprovalStore struct {
	mu   sync.Mutex
	rows map[string]*ifaces.ApprovalRecord
}

func newStubApprovalStore() *stubApprovalStore {
	return &stubApprovalStore{rows: map[string]*ifaces.ApprovalRecord{}}
}

func apKey(tenant, id string) string { return tenant + "/" + id }

func (s *stubApprovalStore) Insert(_ context.Context, r *ifaces.ApprovalRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *r
	s.rows[apKey(r.TenantID, r.ID)] = &cp
	return nil
}

func (s *stubApprovalStore) Get(_ context.Context, tenantID, id string) (*ifaces.ApprovalRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[apKey(tenantID, id)]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (s *stubApprovalStore) ListPending(_ context.Context, tenantID string) ([]*ifaces.ApprovalRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*ifaces.ApprovalRecord{}
	for _, r := range s.rows {
		if r.TenantID == tenantID && r.Status == "pending" {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *stubApprovalStore) UpdateStatus(_ context.Context, tenantID, id, status string, decidedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[apKey(tenantID, id)]
	if !ok {
		return ifaces.ErrNotFound
	}
	r.Status = status
	r.DecidedAt = &decidedAt
	return nil
}

func (s *stubApprovalStore) ExpireOlderThan(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

// stubAuditStore is a minimal in-memory AuditStore for handlers_audit tests.
type stubAuditStore struct {
	mu     sync.Mutex
	events []*ifaces.AuditEvent
	failQ  bool
}

func newStubAuditStore() *stubAuditStore { return &stubAuditStore{} }

func (s *stubAuditStore) Append(_ context.Context, e *ifaces.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *e
	s.events = append(s.events, &cp)
	return nil
}

func (s *stubAuditStore) Query(_ context.Context, q ifaces.AuditQuery) ([]*ifaces.AuditEvent, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failQ {
		return nil, "", errors.New("audit boom")
	}
	out := []*ifaces.AuditEvent{}
	for _, e := range s.events {
		if e.TenantID == q.TenantID {
			cp := *e
			out = append(out, &cp)
		}
	}
	return out, "", nil
}
