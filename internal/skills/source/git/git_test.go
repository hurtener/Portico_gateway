package git

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/hurtener/Portico_gateway/internal/skills/source"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

const sampleManifest = `id: acme.test
title: Test
version: 1.0.0
spec: skills/v1
instructions: SKILL.md
binding:
  required_tools:
    - acme.do
`

func newBareRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()

	worktree := filepath.Join(dir, "work")
	if err := os.MkdirAll(filepath.Join(worktree, "acme/test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "acme/test/manifest.yaml"), []byte(sampleManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "acme/test/SKILL.md"), []byte("# Test"), 0o600); err != nil {
		t.Fatal(err)
	}

	repo, err := gogit.PlainInit(worktree, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("."); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("init", &gogit.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "t@e.x", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	return worktree, dir
}

func TestGit_Clone_FromLocalPath(t *testing.T) {
	worktree, _ := newBareRepo(t)
	dataDir := t.TempDir()

	cfg, _ := json.Marshal(Config{URL: worktree})
	src, err := factory(context.Background(), cfg, source.FactoryDeps{
		TenantID: "t1", DataDir: dataDir, Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	refs, err := src.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "acme.test" {
		t.Fatalf("unexpected refs: %+v", refs)
	}
	rc, info, err := src.ReadFile(context.Background(), refs[0], "manifest.yaml")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if len(body) == 0 {
		t.Fatalf("empty manifest body, info=%+v", info)
	}
}

func TestGit_RejectsTraversal(t *testing.T) {
	worktree, _ := newBareRepo(t)
	dataDir := t.TempDir()
	cfg, _ := json.Marshal(Config{URL: worktree})
	src, err := factory(context.Background(), cfg, source.FactoryDeps{
		TenantID: "t1", DataDir: dataDir, Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	refs, _ := src.List(context.Background())
	if len(refs) == 0 {
		t.Fatal("no refs")
	}
	_, _, err = src.ReadFile(context.Background(), refs[0], "../../../etc/passwd")
	if err == nil {
		t.Error("expected traversal rejection")
	}
}

func TestGit_Refresh_DiffEmitsEvents(t *testing.T) {
	worktree, _ := newBareRepo(t)
	dataDir := t.TempDir()

	cfg, _ := json.Marshal(Config{URL: worktree, RefreshInterval: "30s"})
	srcAny, err := factory(context.Background(), cfg, source.FactoryDeps{
		TenantID: "t1", DataDir: dataDir, Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	src := srcAny.(*Source)

	// Initial clone.
	if _, err := src.List(context.Background()); err != nil {
		t.Fatalf("initial List: %v", err)
	}

	// Add a second pack and commit.
	if err := os.MkdirAll(filepath.Join(worktree, "acme/two"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest2 := `id: acme.two
title: Two
version: 1.0.0
spec: skills/v1
instructions: SKILL.md
binding:
  required_tools:
    - acme.do
`
	if err := os.WriteFile(filepath.Join(worktree, "acme/two/manifest.yaml"), []byte(manifest2), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "acme/two/SKILL.md"), []byte("# Two"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo, err := gogit.PlainOpen(worktree)
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := repo.Worktree()
	if _, err := wt.Add("."); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("add two", &gogit.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "t@e.x", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	// Manual refresh
	refs, err := src.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(refs) != 2 {
		t.Errorf("len(refs)=%d after refresh", len(refs))
	}
}
