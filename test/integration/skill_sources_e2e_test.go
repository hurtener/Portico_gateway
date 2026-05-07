// Phase 8 integration tests: REST contract for /api/skill-sources
// and /api/skills/authored, plus tenant-isolation invariants.

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/server/api"
	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
	"github.com/hurtener/Portico_gateway/internal/skills/source/authored"
	"github.com/hurtener/Portico_gateway/internal/storage"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"

	_ "github.com/hurtener/Portico_gateway/internal/skills/source/git"
	_ "github.com/hurtener/Portico_gateway/internal/skills/source/http"
	_ "github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

type manifestType = manifest.Manifest

// phase8Server wires only the Phase 8 surface so the test stays fast.
type phase8Server struct {
	t      *testing.T
	srv    *httptest.Server
	store  *authored.Store
	repo   ifaces.SkillSourceStore
	logger *slog.Logger
}

func startPhase8Server(t *testing.T) *phase8Server {
	t.Helper()
	cfg := config.StorageConfig{Driver: "sqlite", DSN: ":memory:"}
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	backend, err := storage.Open(context.Background(), cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	store := authored.NewStore(backend.AuthoredSkills(), logger)
	skillSourcesCtl := &phase8SourcesAdapter{store: backend.SkillSources()}
	authoredCtl := &phase8AuthoredAdapter{store: store}

	deps := api.Deps{
		Logger:         logger,
		DevMode:        true,
		DevTenant:      "dev",
		Tenants:        backend.Tenants(),
		Audit:          backend.Audit(),
		SkillSources:   skillSourcesCtl,
		AuthoredSkills: authoredCtl,
		SkillValidator: &phase8Validator{},
	}
	handler := api.NewRouter(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &phase8Server{t: t, srv: srv, store: store, repo: backend.SkillSources(), logger: logger}
}

// phase8SourcesAdapter implements api.SkillSourcesController against
// the SQLite store directly (no Source registry needed for the basic
// REST round-trip test).
type phase8SourcesAdapter struct {
	store ifaces.SkillSourceStore
}

func (a *phase8SourcesAdapter) List(ctx context.Context, tenantID string) ([]*ifaces.SkillSourceRecord, error) {
	return a.store.List(ctx, tenantID)
}
func (a *phase8SourcesAdapter) Get(ctx context.Context, tenantID, name string) (*ifaces.SkillSourceRecord, error) {
	return a.store.Get(ctx, tenantID, name)
}
func (a *phase8SourcesAdapter) Upsert(ctx context.Context, rec *ifaces.SkillSourceRecord) error {
	return a.store.Upsert(ctx, rec)
}
func (a *phase8SourcesAdapter) Delete(ctx context.Context, tenantID, name string) error {
	return a.store.Delete(ctx, tenantID, name)
}
func (a *phase8SourcesAdapter) Refresh(ctx context.Context, tenantID, name string) error {
	return a.store.MarkRefreshed(ctx, tenantID, name, time.Now().UTC(), "")
}
func (a *phase8SourcesAdapter) ListPacks(_ context.Context, _, _ string) ([]api.SourcePack, error) {
	return []api.SourcePack{}, nil
}

// phase8AuthoredAdapter wraps *authored.Store.
type phase8AuthoredAdapter struct {
	store *authored.Store
}

func (a *phase8AuthoredAdapter) ListAuthored(ctx context.Context, tenantID string) ([]authored.Authored, error) {
	return a.store.ListAuthored(ctx, tenantID)
}
func (a *phase8AuthoredAdapter) GetAuthored(ctx context.Context, tenantID, skillID, version string) (*authored.Authored, error) {
	return a.store.GetAuthored(ctx, tenantID, skillID, version)
}
func (a *phase8AuthoredAdapter) History(ctx context.Context, tenantID, skillID string) ([]authored.Authored, error) {
	return a.store.History(ctx, tenantID, skillID)
}
func (a *phase8AuthoredAdapter) GetActive(ctx context.Context, tenantID, skillID string) (*authored.Authored, error) {
	return a.store.GetActive(ctx, tenantID, skillID)
}
func (a *phase8AuthoredAdapter) CreateDraft(ctx context.Context, tenantID, userID string, m manifestType, files []authored.File) (*authored.Authored, error) {
	return a.store.CreateDraft(ctx, tenantID, userID, m, files)
}
func (a *phase8AuthoredAdapter) UpdateDraft(ctx context.Context, tenantID, skillID, version, userID string, m manifestType, files []authored.File) (*authored.Authored, error) {
	return a.store.UpdateDraft(ctx, tenantID, skillID, version, userID, m, files)
}
func (a *phase8AuthoredAdapter) Publish(ctx context.Context, tenantID, skillID, version string) (*authored.Authored, error) {
	return a.store.Publish(ctx, tenantID, skillID, version)
}
func (a *phase8AuthoredAdapter) Archive(ctx context.Context, tenantID, skillID, version string) error {
	return a.store.Archive(ctx, tenantID, skillID, version)
}
func (a *phase8AuthoredAdapter) DeleteDraft(ctx context.Context, tenantID, skillID, version string) error {
	return a.store.DeleteDraft(ctx, tenantID, skillID, version)
}

type phase8Validator struct{}

func (*phase8Validator) Validate(_ []byte) []api.ValidatorViolation { return nil }

// helpers ------------------------------------------------------------

func (s *phase8Server) post(path string, body any) (*http.Response, []byte) {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(http.MethodPost, s.srv.URL+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, out
}

func (s *phase8Server) get(path string) (*http.Response, []byte) {
	req, _ := http.NewRequest(http.MethodGet, s.srv.URL+path, nil)
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, out
}

// TestE2E_AuthoredPublish_VisibleToSource: create a draft, publish it,
// list shows status=published.
func TestE2E_AuthoredPublish_VisibleToSource(t *testing.T) {
	s := startPhase8Server(t)
	body := map[string]any{
		"manifest": `id: acme.test
title: Test
version: 1.0.0
spec: skills/v1
instructions: SKILL.md
binding:
  required_tools:
    - acme.do
`,
		"files": []map[string]string{
			{"relpath": "SKILL.md", "mime_type": "text/markdown", "body": "# Test"},
		},
	}
	resp, out := s.post("/api/skills/authored", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", resp.StatusCode, out)
	}
	resp, out = s.post("/api/skills/authored/acme.test/versions/1.0.0/publish", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish: status=%d body=%s", resp.StatusCode, out)
	}
	resp, out = s.get("/api/skills/authored")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", resp.StatusCode, out)
	}
	var listBody struct {
		Items []struct {
			SkillID string `json:"skill_id"`
			Status  string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &listBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, it := range listBody.Items {
		if it.SkillID == "acme.test" && it.Status == "published" {
			found = true
		}
	}
	if !found {
		t.Errorf("published skill not in list: %s", out)
	}
}

// TestE2E_TenantIsolation_Authored asserts authored skills cannot leak
// across tenants.
func TestE2E_TenantIsolation_Authored(t *testing.T) {
	s := startPhase8Server(t)
	// Use the in-process store directly to seed two tenants (the
	// REST surface is dev-tenant only).
	ctx := context.Background()
	mA := manifestType{ID: "acme.test", Title: "A", Version: "1.0.0", Spec: "skills/v1", Instructions: "SKILL.md"}
	mA.Binding.RequiredTools = []string{"acme.do"}
	if _, err := s.store.CreateDraft(ctx, "tenantA", "u", mA, nil); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := s.store.Publish(ctx, "tenantA", "acme.test", "1.0.0"); err != nil {
		t.Fatalf("publish A: %v", err)
	}
	listB, err := s.store.ListAuthored(ctx, "tenantB")
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(listB) != 0 {
		t.Errorf("tenant B saw %d packs (expected 0)", len(listB))
	}
}

// TestE2E_SkillSources_RoundTrip_DevMode covers POST → GET → DELETE.
func TestE2E_SkillSources_RoundTrip_DevMode(t *testing.T) {
	s := startPhase8Server(t)
	body := map[string]any{
		"name":   "vendor-feed",
		"driver": "http",
		"config": map[string]any{
			"feed_url": "https://example.invalid/feed",
		},
		"refresh_seconds": 300,
		"priority":        50,
		"enabled":         true,
	}
	resp, out := s.post("/api/skill-sources", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("post: %d %s", resp.StatusCode, out)
	}
	resp, out = s.get("/api/skill-sources")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get list: %d %s", resp.StatusCode, out)
	}
	var listBody struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &listBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listBody.Items) != 1 || listBody.Items[0].Name != "vendor-feed" {
		t.Errorf("list: %+v", listBody.Items)
	}
	// DELETE returns 204
	req, _ := http.NewRequest(http.MethodDelete, s.srv.URL+"/api/skill-sources/vendor-feed", nil)
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("delete: %d", resp.StatusCode)
	}
}
