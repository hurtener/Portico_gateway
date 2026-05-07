package http

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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
		{ID: "a", Version: "1", Loc: "a@1"},
		{ID: "b", Version: "2", Loc: "b@2"},
	}
	h := packHashes(refs)
	if len(h) != 2 || h["a"] != "1@a@1" || h["b"] != "2@b@2" {
		t.Errorf("hashes=%v", h)
	}
}

func TestRefFromList_Match(t *testing.T) {
	refs := []source.Ref{{ID: "a"}, {ID: "b"}}
	got := refFromList(refs, "b")
	if got.ID != "b" {
		t.Errorf("got=%v", got)
	}
	miss := refFromList(refs, "z")
	if miss.ID != "z" || miss.Version != "" {
		t.Errorf("missing returns synthetic ref: %v", miss)
	}
}

func TestEmit_DropsWhenFull(t *testing.T) {
	ch := make(chan source.Event, 1)
	ch <- source.Event{Kind: source.EventAdded, Ref: source.Ref{ID: "first"}}
	emit(ch, source.Event{Kind: source.EventAdded, Ref: source.Ref{ID: "second"}}, discardLogger())
	if len(ch) != 1 {
		t.Errorf("expected len(ch)=1 after drop, got %d", len(ch))
	}
}

func TestVerifyChecksum_Cases(t *testing.T) {
	body := []byte("hello")
	// Compute canonical sha256.
	good := "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if err := verifyChecksum(body, good); err != nil {
		t.Errorf("good checksum rejected: %v", err)
	}
	if err := verifyChecksum(body, "sha256:"+hex.EncodeToString(make([]byte, 32))); err == nil {
		t.Error("mismatch not rejected")
	}
	if err := verifyChecksum(body, ""); err == nil {
		t.Error("empty checksum not rejected")
	}
}

func TestDetectMIME_KnownExtensions(t *testing.T) {
	cases := map[string]string{
		"manifest.yaml": "application/yaml",
		"data.json":     "application/json",
		"SKILL.md":      "text/markdown",
		"page.html":     "text/html",
		"notes.txt":     "text/plain",
		"unknown.xyz":   "application/octet-stream",
	}
	for path, want := range cases {
		if got := detectMIME(path); got != want {
			t.Errorf("detectMIME(%q)=%q want %q", path, got, want)
		}
	}
}

func TestTrimBundlePrefix(t *testing.T) {
	if got := trimBundlePrefix("acme.test/manifest.yaml"); got != "manifest.yaml" {
		t.Errorf("got=%q", got)
	}
	if got := trimBundlePrefix("manifest.yaml"); got != "manifest.yaml" {
		t.Errorf("no-slash unchanged: got=%q", got)
	}
}

// --- Name + Open ----------------------------------------------------

func TestName_HonoursSourceName(t *testing.T) {
	cfgJSON, _ := json.Marshal(Config{FeedURL: "http://example.invalid/feed"})
	src, err := factory(context.Background(), cfgJSON, source.FactoryDeps{
		TenantID: "t", DataDir: t.TempDir(), Logger: discardLogger(), SourceName: "myfeed",
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	defer src.(*Source).Stop()
	if src.Name() != "myfeed" {
		t.Errorf("Name()=%q want myfeed", src.Name())
	}
}

func TestOpen_FetchesAndParsesManifest(t *testing.T) {
	bundle, checksum := makeBundle(t)
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path == "/feed" {
			feed := FeedDocument{Schema: "skill-feed/v1", Updated: time.Now().UTC(),
				Packs: []FeedPackEntry{{ID: "acme.test", Version: "1.0.0",
					Checksum: checksum, BundleURL: "http://" + r.Host + "/bundle"}}}
			_ = json.NewEncoder(w).Encode(feed)
			return
		}
		_, _ = w.Write(bundle)
	}))
	defer srv.Close()
	cfgJSON, _ := json.Marshal(Config{FeedURL: srv.URL + "/feed"})
	src, err := factory(context.Background(), cfgJSON, source.FactoryDeps{
		TenantID: "t", DataDir: t.TempDir(), Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	defer src.(*Source).Stop()
	refs, _ := src.List(context.Background())
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

// --- fetchURL edges -------------------------------------------------

type stubVault struct {
	value string
	err   error
}

func (v *stubVault) Get(_ context.Context, _, _ string) (string, error) {
	return v.value, v.err
}

func TestFetchURL_AuthHeaderApplied(t *testing.T) {
	var seenHeader string
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		seenHeader = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"schema":"skill-feed/v1","packs":[]}`))
	}))
	defer srv.Close()
	cfgJSON, _ := json.Marshal(Config{FeedURL: srv.URL, CredentialRef: "tok"})
	src, err := factory(context.Background(), cfgJSON, source.FactoryDeps{
		TenantID: "t", DataDir: t.TempDir(), Logger: discardLogger(),
		Vault: &stubVault{value: "abc123"},
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	defer src.(*Source).Stop()
	if _, err := src.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	if seenHeader != "Bearer abc123" {
		t.Errorf("Authorization=%q want %q", seenHeader, "Bearer abc123")
	}
}

func TestFetchURL_CustomHeaderName(t *testing.T) {
	var seenAPI string
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		seenAPI = r.Header.Get("X-API-Key")
		_, _ = w.Write([]byte(`{"schema":"skill-feed/v1","packs":[]}`))
	}))
	defer srv.Close()
	cfgJSON, _ := json.Marshal(Config{
		FeedURL: srv.URL, CredentialRef: "key",
		HeaderName: "X-API-Key", HeaderPrefix: "",
	})
	src, _ := factory(context.Background(), cfgJSON, source.FactoryDeps{
		TenantID: "t", DataDir: t.TempDir(), Logger: discardLogger(),
		Vault: &stubVault{value: "k1"},
	})
	defer src.(*Source).Stop()
	if _, err := src.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
	if seenAPI != "k1" {
		t.Errorf("X-API-Key=%q want %q", seenAPI, "k1")
	}
}

func TestFetchURL_4xxNoRetry(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(401)
	}))
	defer srv.Close()
	cfgJSON, _ := json.Marshal(Config{FeedURL: srv.URL})
	src, _ := factory(context.Background(), cfgJSON, source.FactoryDeps{
		TenantID: "t", DataDir: t.TempDir(), Logger: discardLogger(),
	})
	defer src.(*Source).Stop()
	_, err := src.List(context.Background())
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("4xx triggered retries: hits=%d (want 1)", h)
	}
}

func TestFetchURL_VaultError_PropagatesTyped(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cfgJSON, _ := json.Marshal(Config{FeedURL: srv.URL, CredentialRef: "tok"})
	src, _ := factory(context.Background(), cfgJSON, source.FactoryDeps{
		TenantID: "t", DataDir: t.TempDir(), Logger: discardLogger(),
		Vault: &stubVault{err: errors.New("not found")},
	})
	defer src.(*Source).Stop()
	if _, err := src.List(context.Background()); err == nil {
		t.Fatal("expected vault error to surface")
	}
}

func TestFetchURL_NoVault_WhenCredentialRefSet(t *testing.T) {
	cfgJSON, _ := json.Marshal(Config{FeedURL: "http://example.invalid", CredentialRef: "tok"})
	src, _ := factory(context.Background(), cfgJSON, source.FactoryDeps{
		TenantID: "t", DataDir: t.TempDir(), Logger: discardLogger(),
		Vault: nil,
	})
	defer src.(*Source).Stop()
	if _, err := src.List(context.Background()); err == nil {
		t.Fatal("expected error when credential_ref set without vault")
	}
}

// --- Watch lifecycle ------------------------------------------------

func TestWatch_StopsCleanly(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		_, _ = w.Write([]byte(`{"schema":"skill-feed/v1","packs":[]}`))
	}))
	defer srv.Close()
	cfgJSON, _ := json.Marshal(Config{FeedURL: srv.URL, RefreshInterval: "60s"})
	src, _ := factory(context.Background(), cfgJSON, source.FactoryDeps{
		TenantID: "t", DataDir: t.TempDir(), Logger: discardLogger(),
	})
	hs := src.(*Source)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := hs.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	// Second Watch errors.
	if _, err := hs.Watch(ctx); err == nil {
		t.Error("second Watch must return already-watching error")
	}
	hs.Stop()
	select {
	case _, open := <-ch:
		if open {
			// drain residual events
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel did not close after Stop")
	}
	// Idempotent
	hs.Stop()
}

// silence unused warnings
var _ = io.Discard
