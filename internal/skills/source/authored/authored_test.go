package authored

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
	"github.com/hurtener/Portico_gateway/internal/skills/source"
	"github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(context.Background(), filepath.Join(dir, "test.db"), discardLogger())
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewStore(db.AuthoredSkills(), discardLogger())
}

func sampleManifest(id, version string) manifest.Manifest {
	return manifest.Manifest{
		ID:           id,
		Title:        "Sample",
		Version:      version,
		Spec:         manifest.SpecVersion,
		Instructions: "SKILL.md",
		Binding: manifest.Binding{
			RequiredTools: []string{"github.get_pull_request"},
		},
	}
}

func TestAuthored_DraftRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := sampleManifest("acme.test", "1.0.0")
	files := []File{{RelPath: "SKILL.md", MIMEType: "text/markdown", Body: []byte("# Hi")}}
	rec, err := s.CreateDraft(ctx, "tenantA", "user1", m, files)
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if rec.Status != "draft" || rec.SkillID != "acme.test" {
		t.Errorf("unexpected: %+v", rec)
	}
	if len(rec.Checksum) == 0 {
		t.Errorf("checksum missing")
	}

	got, err := s.GetAuthored(ctx, "tenantA", "acme.test", "1.0.0")
	if err != nil {
		t.Fatalf("GetAuthored: %v", err)
	}
	if got.Status != "draft" || len(got.Files) != 1 {
		t.Errorf("got: %+v", got)
	}

	list, err := s.ListAuthored(ctx, "tenantA")
	if err != nil {
		t.Fatalf("ListAuthored: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("len(list)=%d", len(list))
	}
}

func TestAuthored_Publish_FlipsActivePointer(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := sampleManifest("acme.test", "1.0.0")
	if _, err := s.CreateDraft(ctx, "tenantA", "user1", m, nil); err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	rec, err := s.Publish(ctx, "tenantA", "acme.test", "1.0.0")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if rec.Status != "published" {
		t.Errorf("status=%q", rec.Status)
	}
	active, err := s.GetActive(ctx, "tenantA", "acme.test")
	if err != nil {
		t.Fatalf("GetActive: %v", err)
	}
	if active.Version != "1.0.0" {
		t.Errorf("active version=%q", active.Version)
	}
}

func TestAuthored_Watch_FiresOnPublish(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tenant := "tenantA"
	src, err := factory(ctx, []byte(`{}`), source.FactoryDeps{
		AuthoredRepo: s, TenantID: tenant, Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	ch, err := src.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	m := sampleManifest("acme.x", "1.0.0")
	if _, err := s.CreateDraft(ctx, tenant, "u", m, nil); err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if _, err := s.Publish(ctx, tenant, "acme.x", "1.0.0"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	select {
	case ev := <-ch:
		if ev.Kind != source.EventAdded {
			t.Errorf("kind=%v", ev.Kind)
		}
		if ev.Ref.ID != "acme.x" {
			t.Errorf("id=%q", ev.Ref.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not fire")
	}
}

func TestAuthored_TenantIsolation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := sampleManifest("acme.test", "1.0.0")
	if _, err := s.CreateDraft(ctx, "tenantA", "u1", m, nil); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := s.Publish(ctx, "tenantA", "acme.test", "1.0.0"); err != nil {
		t.Fatalf("publish A: %v", err)
	}
	listB, err := s.ListAuthored(ctx, "tenantB")
	if err != nil {
		t.Fatalf("ListAuthored B: %v", err)
	}
	if len(listB) != 0 {
		t.Errorf("tenant B saw %d packs (expected 0)", len(listB))
	}

	// Same skill_id under both tenants must coexist.
	if _, err := s.CreateDraft(ctx, "tenantB", "u2", m, nil); err != nil {
		t.Fatalf("create B same id: %v", err)
	}
	listA, err := s.ListAuthored(ctx, "tenantA")
	if err != nil {
		t.Fatalf("ListAuthored A: %v", err)
	}
	if len(listA) != 1 {
		t.Errorf("tenant A list=%d", len(listA))
	}
}

func TestAuthored_Canonical_HashStableAcrossOSEncodings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := sampleManifest("acme.test", "1.0.0")
	files := []File{
		{RelPath: "SKILL.md", MIMEType: "text/markdown", Body: []byte("alpha")},
		{RelPath: "prompts/x.md", MIMEType: "text/markdown", Body: []byte("beta")},
	}
	rec1, err := s.CreateDraft(ctx, "t1", "u", m, files)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Reorder files: the canonical hash MUST be identical because we
	// sort by RelPath.
	reorder := []File{files[1], files[0]}
	rec2, err := s.CreateDraft(ctx, "t2", "u", m, reorder)
	if err != nil {
		t.Fatalf("create2: %v", err)
	}
	if rec1.Checksum != rec2.Checksum {
		t.Errorf("checksum drift: %q vs %q", rec1.Checksum, rec2.Checksum)
	}
}

func TestAuthored_DeleteDraft_RefusesPublished(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := sampleManifest("acme.test", "1.0.0")
	if _, err := s.CreateDraft(ctx, "t1", "u", m, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Publish(ctx, "t1", "acme.test", "1.0.0"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := s.DeleteDraft(ctx, "t1", "acme.test", "1.0.0"); err == nil {
		t.Error("expected ErrNotFound on draft delete of published version")
	}
}
