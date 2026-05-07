package http

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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

func makeBundle(t *testing.T) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string]string{
		"acme.test/manifest.yaml": sampleManifest,
		"acme.test/SKILL.md":      "# Test\n",
	}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	bytes := buf.Bytes()
	h := sha256.Sum256(bytes)
	return bytes, "sha256:" + hex.EncodeToString(h[:])
}

func TestHTTP_FeedDecode_Valid(t *testing.T) {
	bundle, checksum := makeBundle(t)
	feed := FeedDocument{
		Schema:  "skill-feed/v1",
		Updated: time.Now().UTC(),
		Packs: []FeedPackEntry{
			{ID: "acme.test", Version: "1.0.0", Checksum: checksum, BundleURL: ""},
		},
	}

	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path == "/feed" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(feed)
			return
		}
		if r.URL.Path == "/bundle.tgz" {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(bundle)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()
	feed.Packs[0].BundleURL = srv.URL + "/bundle.tgz"

	cfgJSON, _ := json.Marshal(Config{FeedURL: srv.URL + "/feed"})
	src, err := factory(context.Background(), cfgJSON, source.FactoryDeps{
		TenantID: "t", DataDir: t.TempDir(), Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	defer src.(*Source).Stop()

	// Re-encode the feed with the populated bundle URL.
	srvFeedSrv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_ = json.NewEncoder(w).Encode(feed)
	}))
	defer srvFeedSrv.Close()
	src.(*Source).cfg.FeedURL = srv.URL + "/feed"

	refs, err := src.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "acme.test" {
		t.Fatalf("unexpected refs: %+v", refs)
	}

	rc, _, err := src.ReadFile(context.Background(), refs[0], "manifest.yaml")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if !bytes.Contains(body, []byte("acme.test")) {
		t.Errorf("manifest body unexpected: %q", string(body))
	}
}

func TestHTTP_FeedDecode_BadSchema_TypedError(t *testing.T) {
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		_, _ = w.Write([]byte(`{"not_a_feed":true}`))
	}))
	defer srv.Close()
	cfgJSON, _ := json.Marshal(Config{FeedURL: srv.URL})
	src, err := factory(context.Background(), cfgJSON, source.FactoryDeps{
		TenantID: "t", DataDir: t.TempDir(), Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	defer src.(*Source).Stop()
	_, err = src.List(context.Background())
	if err == nil {
		t.Fatal("expected error on missing schema")
	}
}

func TestHTTP_BundleFetch_5xxRetries(t *testing.T) {
	bundle, checksum := makeBundle(t)
	var failsLeft int32 = 2
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path == "/feed" {
			feed := FeedDocument{Schema: "skill-feed/v1", Updated: time.Now().UTC(),
				Packs: []FeedPackEntry{{ID: "acme.test", Version: "1.0.0", Checksum: checksum, BundleURL: "/bundle"}}}
			feed.Packs[0].BundleURL = "http://" + r.Host + "/bundle"
			_ = json.NewEncoder(w).Encode(feed)
			return
		}
		if atomic.AddInt32(&failsLeft, -1) >= 0 {
			w.WriteHeader(503)
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
	refs, err := src.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("expected ≥1 ref")
	}
	_, _, err = src.ReadFile(context.Background(), refs[0], "manifest.yaml")
	if err != nil {
		t.Fatalf("ReadFile after retries: %v", err)
	}
}

func TestHTTP_ChecksumMismatch_Refuses(t *testing.T) {
	bundle, _ := makeBundle(t)
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.URL.Path == "/feed" {
			feed := FeedDocument{Schema: "skill-feed/v1", Updated: time.Now().UTC(),
				Packs: []FeedPackEntry{{ID: "acme.test", Version: "1.0.0",
					Checksum: "sha256:" + hex.EncodeToString(make([]byte, 32)), BundleURL: "/bundle"}}}
			feed.Packs[0].BundleURL = "http://" + r.Host + "/bundle"
			_ = json.NewEncoder(w).Encode(feed)
			return
		}
		_, _ = w.Write(bundle)
	}))
	defer srv.Close()
	cfgJSON, _ := json.Marshal(Config{FeedURL: srv.URL + "/feed"})
	src, _ := factory(context.Background(), cfgJSON, source.FactoryDeps{
		TenantID: "t", DataDir: t.TempDir(), Logger: discardLogger(),
	})
	defer src.(*Source).Stop()
	refs, _ := src.List(context.Background())
	if len(refs) == 0 {
		t.Fatal("List returned 0")
	}
	_, _, err := src.ReadFile(context.Background(), refs[0], "manifest.yaml")
	if err == nil {
		t.Error("expected checksum mismatch error")
	}
}
