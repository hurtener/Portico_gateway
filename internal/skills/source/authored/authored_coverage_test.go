package authored

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/skills/source"
)

// --- Source interface (List / Open / ReadFile) ----------------------

func TestSource_List_PublishedOnly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t1"

	// One published, one draft → List returns only the published.
	if _, err := s.CreateDraft(ctx, tenant, "u", sampleManifest("acme.pub", "1.0.0"), nil); err != nil {
		t.Fatalf("draft1: %v", err)
	}
	if _, err := s.Publish(ctx, tenant, "acme.pub", "1.0.0"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := s.CreateDraft(ctx, tenant, "u", sampleManifest("acme.draft", "0.1.0"), nil); err != nil {
		t.Fatalf("draft2: %v", err)
	}

	src, err := factory(ctx, []byte(`{}`), source.FactoryDeps{
		AuthoredRepo: s, TenantID: tenant, Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	refs, err := src.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("List returned %d refs, want 1", len(refs))
	}
	if refs[0].ID != "acme.pub" {
		t.Errorf("expected acme.pub published, got %q", refs[0].ID)
	}
	if refs[0].Source != SourceName {
		t.Errorf("Source=%q want %q", refs[0].Source, SourceName)
	}
}

func TestSource_Open_ParsesManifest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t1"
	files := []File{{RelPath: "SKILL.md", MIMEType: "text/markdown", Body: []byte("# Hi")}}
	if _, err := s.CreateDraft(ctx, tenant, "u", sampleManifest("acme.test", "1.0.0"), files); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Publish(ctx, tenant, "acme.test", "1.0.0"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	src, err := factory(ctx, []byte(`{}`), source.FactoryDeps{
		AuthoredRepo: s, TenantID: tenant, Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	refs, _ := src.List(ctx)
	if len(refs) == 0 {
		t.Fatal("no refs")
	}
	m, err := src.Open(ctx, refs[0])
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if m.ID != "acme.test" {
		t.Errorf("manifest.ID=%q", m.ID)
	}
	if m.Title == "" {
		t.Errorf("manifest.Title empty")
	}
}

func TestSource_ReadFile_ManifestAndContent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t1"
	files := []File{
		{RelPath: "SKILL.md", MIMEType: "text/markdown", Body: []byte("# Hi")},
		{RelPath: "prompts/triage.md", MIMEType: "text/markdown", Body: []byte("triage")},
	}
	if _, err := s.CreateDraft(ctx, tenant, "u", sampleManifest("acme.test", "1.0.0"), files); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Publish(ctx, tenant, "acme.test", "1.0.0"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	src, _ := factory(ctx, []byte(`{}`), source.FactoryDeps{
		AuthoredRepo: s, TenantID: tenant, Logger: discardLogger(),
	})
	refs, _ := src.List(ctx)

	// manifest.yaml special path: served from canonical JSON.
	rc, info, err := src.ReadFile(ctx, refs[0], "manifest.yaml")
	if err != nil {
		t.Fatalf("ReadFile manifest: %v", err)
	}
	body, _ := io.ReadAll(rc)
	rc.Close()
	if !strings.Contains(string(body), `"acme.test"`) {
		t.Errorf("manifest body unexpected: %q", string(body))
	}
	if info.MIMEType != "application/yaml" {
		t.Errorf("MIME=%q", info.MIMEType)
	}

	// regular file
	rc, info, err = src.ReadFile(ctx, refs[0], "SKILL.md")
	if err != nil {
		t.Fatalf("ReadFile SKILL.md: %v", err)
	}
	body, _ = io.ReadAll(rc)
	rc.Close()
	if string(body) != "# Hi" {
		t.Errorf("SKILL.md body=%q", string(body))
	}
	if info.Size != int64(len("# Hi")) {
		t.Errorf("size=%d", info.Size)
	}

	// nested prompts file
	rc, _, err = src.ReadFile(ctx, refs[0], "prompts/triage.md")
	if err != nil {
		t.Fatalf("ReadFile nested: %v", err)
	}
	rc.Close()
}

func TestSource_ReadFile_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t1"
	if _, err := s.CreateDraft(ctx, tenant, "u", sampleManifest("acme.test", "1.0.0"), nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Publish(ctx, tenant, "acme.test", "1.0.0"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	src, _ := factory(ctx, []byte(`{}`), source.FactoryDeps{
		AuthoredRepo: s, TenantID: tenant, Logger: discardLogger(),
	})
	refs, _ := src.List(ctx)
	if _, _, err := src.ReadFile(ctx, refs[0], "missing.txt"); err == nil {
		t.Error("expected not-found error")
	}
}

func TestSource_Open_Missing_ReturnsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	src, _ := factory(ctx, []byte(`{}`), source.FactoryDeps{
		AuthoredRepo: s, TenantID: "t1", Logger: discardLogger(),
	})
	if _, err := src.Open(ctx, source.Ref{ID: "nope", Version: "0.0.0"}); err == nil {
		t.Error("expected error opening missing pack")
	}
}

// --- Repo CRUD: History / UpdateDraft / Archive / GetActive --------

func TestHistory_AcrossStatuses(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t1"

	if _, err := s.CreateDraft(ctx, tenant, "u", sampleManifest("acme.test", "1.0.0"), nil); err != nil {
		t.Fatalf("create v1: %v", err)
	}
	if _, err := s.Publish(ctx, tenant, "acme.test", "1.0.0"); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	if _, err := s.CreateDraft(ctx, tenant, "u", sampleManifest("acme.test", "1.1.0"), nil); err != nil {
		t.Fatalf("create v1.1: %v", err)
	}

	hist, err := s.History(ctx, tenant, "acme.test")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(hist) < 2 {
		t.Errorf("expected ≥2 revisions, got %d", len(hist))
	}
}

func TestUpdateDraft_HappyPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t1"
	if _, err := s.CreateDraft(ctx, tenant, "u", sampleManifest("acme.test", "1.0.0"), nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	files := []File{{RelPath: "SKILL.md", MIMEType: "text/markdown", Body: []byte("revised")}}
	got, err := s.UpdateDraft(ctx, tenant, "acme.test", "1.0.0", "u", sampleManifest("acme.test", "1.0.0"), files)
	if err != nil {
		t.Fatalf("UpdateDraft: %v", err)
	}
	if got.Status != "draft" {
		t.Errorf("status=%q", got.Status)
	}
}

func TestUpdateDraft_RejectsMismatchedManifest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t1"
	if _, err := s.CreateDraft(ctx, tenant, "u", sampleManifest("acme.test", "1.0.0"), nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	mismatched := sampleManifest("other.skill", "9.9.9")
	if _, err := s.UpdateDraft(ctx, tenant, "acme.test", "1.0.0", "u", mismatched, nil); err == nil {
		t.Error("expected mismatch error")
	}
}

func TestUpdateDraft_RequiresPathFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.UpdateDraft(ctx, "", "x", "1", "u", sampleManifest("x", "1"), nil); err == nil {
		t.Error("missing tenant should reject")
	}
}

func TestArchive_EmitsRemoved(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tenant := "t1"

	if _, err := s.CreateDraft(ctx, tenant, "u", sampleManifest("acme.test", "1.0.0"), nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Publish(ctx, tenant, "acme.test", "1.0.0"); err != nil {
		t.Fatalf("publish: %v", err)
	}

	src, _ := factory(ctx, []byte(`{}`), source.FactoryDeps{
		AuthoredRepo: s, TenantID: tenant, Logger: discardLogger(),
	})
	ch, err := src.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	if err := s.Archive(ctx, tenant, "acme.test", "1.0.0"); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// drain until we see the removed event or time out
	var saw bool
	for i := 0; i < 4 && !saw; i++ {
		select {
		case ev := <-ch:
			if ev.Kind == source.EventRemoved {
				saw = true
			}
		default:
		}
	}
	if !saw {
		// fallback: peek at one more event with a short blocking read
		select {
		case ev := <-ch:
			if ev.Kind == source.EventRemoved {
				saw = true
			}
		default:
		}
	}
	if !saw {
		t.Error("expected EventRemoved after Archive")
	}
}

func TestGetActive_NoPublished_ReturnsError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.GetActive(ctx, "t1", "missing"); err == nil {
		t.Error("expected error when no published version exists")
	}
}

// --- AuthoredRepo surface (ListPublished/LoadManifest/LoadFile/Subscribe) ---

func TestStoreSubscriberSurface_ListPublished_LoadManifest_LoadFile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t1"
	files := []File{{RelPath: "SKILL.md", MIMEType: "text/markdown", Body: []byte("# H")}}
	if _, err := s.CreateDraft(ctx, tenant, "u", sampleManifest("acme.test", "1.0.0"), files); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Publish(ctx, tenant, "acme.test", "1.0.0"); err != nil {
		t.Fatalf("publish: %v", err)
	}

	handles, err := s.ListPublished(ctx, tenant)
	if err != nil {
		t.Fatalf("ListPublished: %v", err)
	}
	if len(handles) != 1 || handles[0].SkillID != "acme.test" {
		t.Errorf("handles=%+v", handles)
	}

	manifest, err := s.LoadManifest(ctx, tenant, "acme.test", "1.0.0")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if !strings.Contains(string(manifest), "acme.test") {
		t.Errorf("manifest missing id")
	}

	body, mime, err := s.LoadFile(ctx, tenant, "acme.test", "1.0.0", "SKILL.md")
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if string(body) != "# H" || mime != "text/markdown" {
		t.Errorf("body=%q mime=%q", string(body), mime)
	}
	if _, _, err := s.LoadFile(ctx, tenant, "acme.test", "1.0.0", "nope"); err == nil {
		t.Error("expected not-found")
	}
}

func TestStore_SubscribePublishes_DelegatesToSubscribe(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := s.SubscribePublishes(ctx, "t1")
	if err != nil {
		t.Fatalf("SubscribePublishes: %v", err)
	}
	if ch == nil {
		t.Fatal("nil channel")
	}
	cancel()
}

// --- factory error paths --------------------------------------------

func TestFactory_RejectsMissingAuthoredRepo(t *testing.T) {
	if _, err := factory(context.Background(), []byte(`{}`), source.FactoryDeps{
		TenantID: "t1", Logger: discardLogger(),
	}); err == nil {
		t.Error("expected error when AuthoredRepo missing")
	}
}

// wrongRepo implements source.AuthoredRepo but is NOT *Store, so the
// factory's type assertion must fail.
type wrongRepo struct{}

func (wrongRepo) ListPublished(_ context.Context, _ string) ([]source.AuthoredHandle, error) {
	return nil, nil
}
func (wrongRepo) LoadManifest(_ context.Context, _, _, _ string) ([]byte, error) {
	return nil, nil
}
func (wrongRepo) LoadFile(_ context.Context, _, _, _, _ string) ([]byte, string, error) {
	return nil, "", nil
}
func (wrongRepo) SubscribePublishes(_ context.Context, _ string) (<-chan source.Event, error) {
	return nil, nil
}

func TestFactory_RejectsWrongRepoType(t *testing.T) {
	if _, err := factory(context.Background(), []byte(`{}`), source.FactoryDeps{
		AuthoredRepo: wrongRepo{},
		TenantID:     "t1", Logger: discardLogger(),
	}); err == nil {
		t.Error("expected error for wrong AuthoredRepo type")
	}
}

func TestCreateDraft_RejectsEmptyTenant(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateDraft(context.Background(), "", "u", sampleManifest("x", "1"), nil); err == nil {
		t.Error("expected error for empty tenant")
	}
}

func TestCreateDraft_RejectsEmptyManifestFields(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateDraft(context.Background(), "t1", "u", sampleManifest("", "1"), nil); err == nil {
		t.Error("expected error for empty id")
	}
	if _, err := s.CreateDraft(context.Background(), "t1", "u", sampleManifest("x", ""), nil); err == nil {
		t.Error("expected error for empty version")
	}
}
