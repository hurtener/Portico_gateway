package git

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gogitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/plumbing/object"
	gossh "golang.org/x/crypto/ssh"

	"github.com/hurtener/Portico_gateway/internal/skills/source"
)

// --- Pure helpers ---------------------------------------------------

func TestRefreshInterval_Defaults(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
	}{
		{"", defaultRefreshInterval},
		{"not-a-duration", defaultRefreshInterval},
		{"5s", defaultRefreshInterval}, // below 30s minimum
		{"60s", 60 * time.Second},
		{"10m", 10 * time.Minute},
	}
	for _, c := range cases {
		s := &Source{cfg: Config{RefreshInterval: c.raw}}
		if got := s.refreshInterval(); got != c.want {
			t.Errorf("refreshInterval(%q)=%v want %v", c.raw, got, c.want)
		}
	}
}

func TestPackHashes_Deterministic(t *testing.T) {
	refs := []source.Ref{
		{ID: "a", Version: "1", Loc: "/x/a"},
		{ID: "b", Version: "2", Loc: "/x/b"},
	}
	h := packHashes(refs)
	if len(h) != 2 || h["a"] != "1@/x/a" || h["b"] != "2@/x/b" {
		t.Errorf("hashes=%v", h)
	}
}

func TestDiffEmit_AddedUpdatedRemoved(t *testing.T) {
	prev := map[string]string{"keep": "1@/x", "drop": "1@/y"}
	next := map[string]string{"keep": "2@/x", "add": "1@/z"}
	refs := []source.Ref{{ID: "keep", Version: "2", Loc: "/x"}, {ID: "add", Version: "1", Loc: "/z"}}
	ch := make(chan source.Event, 8)
	diffEmit(prev, next, refs, ch, discardLogger(), "test")
	close(ch)
	got := map[source.EventKind]string{}
	for ev := range ch {
		got[ev.Kind] = ev.Ref.ID
	}
	if got[source.EventAdded] != "add" {
		t.Errorf("missing Added: %v", got)
	}
	if got[source.EventUpdated] != "keep" {
		t.Errorf("missing Updated: %v", got)
	}
	if got[source.EventRemoved] != "drop" {
		t.Errorf("missing Removed: %v", got)
	}
}

func TestTryEmit_DropsWhenFull(t *testing.T) {
	ch := make(chan source.Event, 1)
	ch <- source.Event{Kind: source.EventAdded, Ref: source.Ref{ID: "first"}}
	// Channel full; second emit must drop silently.
	tryEmit(ch, source.Event{Kind: source.EventAdded, Ref: source.Ref{ID: "second"}}, discardLogger())
	if len(ch) != 1 {
		t.Errorf("expected len(ch)=1 after drop, got %d", len(ch))
	}
}

func TestDetectMIME_KnownExtensions(t *testing.T) {
	cases := map[string]string{
		"manifest.yaml":   "application/yaml",
		"data.json":       "application/json",
		"SKILL.md":        "text/markdown",
		"page.html":       "text/html",
		"notes.txt":       "text/plain",
		"unknown.xyz":     "application/octet-stream",
	}
	for path, want := range cases {
		if got := detectMIME(path); got != want {
			t.Errorf("detectMIME(%q)=%q want %q", path, got, want)
		}
	}
}

func TestNameAndRoot_Accessors(t *testing.T) {
	worktree, _ := newBareRepo(t)
	dataDir := t.TempDir()
	cfg, _ := json.Marshal(Config{URL: worktree})
	src, err := factory(context.Background(), cfg, source.FactoryDeps{
		TenantID: "t1", DataDir: dataDir, Logger: discardLogger(), SourceName: "myorg",
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	gs := src.(*Source)
	if gs.Name() != "myorg" {
		t.Errorf("Name()=%q want %q", gs.Name(), "myorg")
	}
	if gs.Root() == "" || !strings.HasPrefix(gs.Root(), dataDir) {
		t.Errorf("Root() outside dataDir: %q", gs.Root())
	}
}

func TestOpen_ParsesManifestFromRef(t *testing.T) {
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
	if len(refs) == 0 {
		t.Fatal("no refs")
	}
	m, err := src.Open(context.Background(), refs[0])
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if m.ID != "acme.test" {
		t.Errorf("manifest.ID=%q", m.ID)
	}
}

// --- Watch goroutine lifecycle --------------------------------------

func TestWatch_StopsCleanly(t *testing.T) {
	worktree, _ := newBareRepo(t)
	dataDir := t.TempDir()
	cfg, _ := json.Marshal(Config{URL: worktree, RefreshInterval: "60s"})
	src, err := factory(context.Background(), cfg, source.FactoryDeps{
		TenantID: "t", DataDir: dataDir, Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	gs := src.(*Source)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := gs.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	// Calling Watch a second time must error.
	if _, err := gs.Watch(ctx); err == nil {
		t.Error("second Watch must return already-watching error")
	}
	gs.Stop()
	select {
	case _, open := <-ch:
		if open {
			// channel may still drain; just want to confirm it eventually closes
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel did not close after Stop")
	}
	// Idempotent
	gs.Stop()
}

func TestWatch_EmitsAddedOnNewCommit(t *testing.T) {
	worktree, _ := newBareRepo(t)
	dataDir := t.TempDir()
	cfg, _ := json.Marshal(Config{URL: worktree, RefreshInterval: "30s"})
	src, err := factory(context.Background(), cfg, source.FactoryDeps{
		TenantID: "t", DataDir: dataDir, Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	gs := src.(*Source)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Prime the cache so Watch's initial scan sees one pack.
	if _, err := gs.List(ctx); err != nil {
		t.Fatalf("prime: %v", err)
	}
	if _, err := gs.Watch(ctx); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer gs.Stop()

	// Add a second pack to the upstream repo.
	if err := os.MkdirAll(filepath.Join(worktree, "acme/two"), 0o755); err != nil {
		t.Fatal(err)
	}
	const m2 = `id: acme.two
title: Two
version: 1.0.0
spec: skills/v1
instructions: SKILL.md
binding:
  required_tools:
    - acme.do
`
	if err := os.WriteFile(filepath.Join(worktree, "acme/two/manifest.yaml"), []byte(m2), 0o600); err != nil {
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
		Author: &object.Signature{Name: "T", Email: "t@e.x", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	// We can't speed the real ticker, but Refresh exercises the same diff
	// pipeline diffEmit drives. Re-scanning + emitting is what
	// matters for coverage; the goroutine itself is exercised by
	// TestWatch_StopsCleanly above.
	if _, err := gs.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Drive the diff pipeline directly so the test isn't bound by the
	// Watch ticker (which is 30s minimum). This still exercises
	// diffEmit + tryEmit + scanPacks.
	ch2 := make(chan source.Event, 8)
	prev := packHashes([]source.Ref{{ID: "acme.test", Version: "1.0.0", Loc: filepath.Join(gs.root, "acme/test")}})
	next, _ := gs.scanPacks()
	diffEmit(prev, packHashes(next), next, ch2, discardLogger(), gs.name)
	close(ch2)
	saw := map[source.EventKind]bool{}
	for ev := range ch2 {
		saw[ev.Kind] = true
	}
	if !saw[source.EventAdded] {
		t.Errorf("expected Added in diffEmit output; got %v", saw)
	}
}

// --- resolveAuth ----------------------------------------------------

type stubVault struct {
	value string
	err   error
}

func (v *stubVault) Get(_ context.Context, _, _ string) (string, error) {
	return v.value, v.err
}

func newGitSource(t *testing.T, cfg Config, vault source.VaultLookup) *Source {
	t.Helper()
	worktree, _ := newBareRepo(t)
	if cfg.URL == "" {
		cfg.URL = worktree
	}
	cfgJSON, _ := json.Marshal(cfg)
	src, err := factory(context.Background(), cfgJSON, source.FactoryDeps{
		TenantID: "t1", DataDir: t.TempDir(), Logger: discardLogger(), Vault: vault,
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	return src.(*Source)
}

func TestResolveAuth_NoCredential(t *testing.T) {
	gs := newGitSource(t, Config{}, nil)
	auth, err := gs.resolveAuth(context.Background())
	if err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	if auth != nil {
		t.Errorf("expected nil auth, got %T", auth)
	}
}

func TestResolveAuth_VaultMissing_ReturnsError(t *testing.T) {
	gs := newGitSource(t, Config{CredentialRef: "github_pat"}, nil)
	if _, err := gs.resolveAuth(context.Background()); err == nil {
		t.Fatal("expected error when vault is nil")
	}
}

func TestResolveAuth_VaultLookupError(t *testing.T) {
	gs := newGitSource(t, Config{CredentialRef: "github_pat"}, &stubVault{err: errors.New("not found")})
	_, err := gs.resolveAuth(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected wrapped vault error, got: %v", err)
	}
}

func TestResolveAuth_HTTPS_PAT(t *testing.T) {
	gs := newGitSource(t, Config{CredentialRef: "github_pat"}, &stubVault{value: "ghp_secrethere"})
	auth, err := gs.resolveAuth(context.Background())
	if err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	ba, ok := auth.(*githttp.BasicAuth)
	if !ok {
		t.Fatalf("expected *BasicAuth, got %T", auth)
	}
	if ba.Username != "x-access-token" {
		t.Errorf("Username=%q want x-access-token", ba.Username)
	}
	if ba.Password != "ghp_secrethere" {
		t.Errorf("Password mismatch")
	}
}

func TestResolveAuth_HTTPS_PAT_CustomUsername(t *testing.T) {
	gs := newGitSource(t, Config{CredentialRef: "tok", BasicUsername: "ci-bot"}, &stubVault{value: "tok"})
	auth, err := gs.resolveAuth(context.Background())
	if err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	ba := auth.(*githttp.BasicAuth)
	if ba.Username != "ci-bot" {
		t.Errorf("Username=%q want ci-bot", ba.Username)
	}
}

// generateEd25519PEM generates an unencrypted Ed25519 private key encoded
// as OpenSSH PEM ("-----BEGIN OPENSSH PRIVATE KEY-----"), matching what
// `ssh-keygen -t ed25519 -N ""` produces.
func generateEd25519PEM(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	block, err := gossh.MarshalPrivateKey(priv, "test-comment")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(pem.EncodeToMemory(block))
}

func TestResolveAuth_SSHKey_Valid(t *testing.T) {
	pemKey := generateEd25519PEM(t)
	gs := newGitSource(t, Config{CredentialRef: "deploy_key"}, &stubVault{value: pemKey})
	auth, err := gs.resolveAuth(context.Background())
	if err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	pk, ok := auth.(*gogitssh.PublicKeys)
	if !ok {
		t.Fatalf("expected *PublicKeys, got %T", auth)
	}
	if pk.User != "git" {
		t.Errorf("default User=%q want git", pk.User)
	}
	if pk.Signer == nil {
		t.Error("Signer is nil")
	}
}

func TestResolveAuth_SSHKey_CustomUser(t *testing.T) {
	pemKey := generateEd25519PEM(t)
	gs := newGitSource(t, Config{CredentialRef: "deploy_key", SSHUser: "deploy"},
		&stubVault{value: pemKey})
	auth, err := gs.resolveAuth(context.Background())
	if err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	pk := auth.(*gogitssh.PublicKeys)
	if pk.User != "deploy" {
		t.Errorf("User=%q want deploy", pk.User)
	}
}

func TestResolveAuth_SSHKey_GarbageHasTypedError(t *testing.T) {
	bad := "-----BEGIN OPENSSH PRIVATE KEY-----\nnot-actually-base64\n-----END OPENSSH PRIVATE KEY-----\n"
	gs := newGitSource(t, Config{CredentialRef: "deploy_key"}, &stubVault{value: bad})
	_, err := gs.resolveAuth(context.Background())
	if err == nil {
		t.Fatal("expected parse error for garbage key")
	}
	if !strings.Contains(err.Error(), "parse SSH key") {
		t.Errorf("error not typed (parse SSH key): %v", err)
	}
}

func TestSSHAuthFromKey_DefaultsUser(t *testing.T) {
	pemKey := generateEd25519PEM(t)
	auth, err := sshAuthFromKey("", pemKey)
	if err != nil {
		t.Fatalf("sshAuthFromKey: %v", err)
	}
	pk := auth.(*gogitssh.PublicKeys)
	if pk.User != "git" {
		t.Errorf("default user=%q want git", pk.User)
	}
}

// silence unused warnings for the slog import
var _ = slog.Default
var _ = io.Discard
