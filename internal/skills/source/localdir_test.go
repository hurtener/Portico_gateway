package source

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

const sampleManifest = `id: github.code-review
title: GitHub Code Review
version: 0.1.0
spec: skills/v1
instructions: ./SKILL.md
binding:
  required_tools:
    - github.get_pull_request
`

const sampleManifest2 = `id: postgres.sql-analyst
title: Postgres SQL Analyst
version: 0.1.0
spec: skills/v1
instructions: ./SKILL.md
binding:
  required_tools:
    - postgres.run_sql
`

func writePack(t *testing.T, root, ns, name, manifestBody string) string {
	t.Helper()
	dir := filepath.Join(root, ns, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifestBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# How to use\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestList_DiscoversTwoPacks(t *testing.T) {
	root := t.TempDir()
	writePack(t, root, "github", "code-review", sampleManifest)
	writePack(t, root, "postgres", "sql-analyst", sampleManifest2)

	src, err := NewLocalDir(root, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	refs, err := src.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 packs; got %d (%v)", len(refs), refs)
	}
	seen := map[string]bool{}
	for _, r := range refs {
		seen[r.ID] = true
		if r.Source != "local" {
			t.Errorf("source = %q", r.Source)
		}
	}
	if !seen["github.code-review"] || !seen["postgres.sql-analyst"] {
		t.Errorf("missing pack id: seen=%v", seen)
	}
}

func TestOpen_ReturnsManifest(t *testing.T) {
	root := t.TempDir()
	writePack(t, root, "github", "code-review", sampleManifest)
	src, _ := NewLocalDir(root, discardLogger())
	refs, _ := src.List(context.Background())
	m, err := src.Open(context.Background(), refs[0])
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "github.code-review" {
		t.Errorf("id = %q", m.ID)
	}
}

func TestReadFile_TraversalRejected(t *testing.T) {
	root := t.TempDir()
	pack := writePack(t, root, "x", "y", sampleManifest)
	src, _ := NewLocalDir(root, discardLogger())
	ref := Ref{ID: "x.y", Source: "local", Loc: pack}

	for _, evil := range []string{"../../etc/passwd", "/etc/passwd", "./../../something"} {
		_, _, err := src.ReadFile(context.Background(), ref, evil)
		if err == nil {
			t.Errorf("expected rejection for %q", evil)
		}
	}
}

func TestReadFile_ReadsRegularFile(t *testing.T) {
	root := t.TempDir()
	pack := writePack(t, root, "x", "y", sampleManifest)
	src, _ := NewLocalDir(root, discardLogger())
	ref := Ref{ID: "x.y", Source: "local", Loc: pack}

	rc, info, err := src.ReadFile(context.Background(), ref, "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "# How to use\n" {
		t.Errorf("body = %q", body)
	}
	if info.MIMEType != "text/markdown" {
		t.Errorf("mime = %q", info.MIMEType)
	}
}

func TestWatch_FiresOnManifestChange(t *testing.T) {
	root := t.TempDir()
	pack := writePack(t, root, "x", "y", sampleManifest)
	src, _ := NewLocalDir(root, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events, err := src.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Give the watcher a tick to register.
	time.Sleep(80 * time.Millisecond)

	if err := os.WriteFile(filepath.Join(pack, "manifest.yaml"), []byte(sampleManifest+"\n# touched\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-events:
		if ev.Kind != EventUpdated {
			t.Errorf("kind = %q", ev.Kind)
		}
		if ev.Ref.ID != "x.y" {
			t.Errorf("id = %q", ev.Ref.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watcher never emitted")
	}
}
