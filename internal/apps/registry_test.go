package apps

import (
	"strings"
	"testing"
	"time"
)

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := New(CSPConfig{})
	app := &App{URI: "ui://github/x.html", ServerID: "github", UpstreamURI: "ui://x.html"}
	r.Register(app)
	got, ok := r.Lookup("ui://github/x.html")
	if !ok || got.ServerID != "github" {
		t.Fatalf("lookup miss: %+v ok=%v", got, ok)
	}
	if got.DiscoveredAt.IsZero() {
		t.Errorf("DiscoveredAt should default")
	}
}

func TestRegistry_RegisterIdempotent(t *testing.T) {
	r := New(CSPConfig{})
	first := time.Now().Add(-time.Hour).UTC()
	r.Register(&App{URI: "ui://a/b", ServerID: "a", DiscoveredAt: first})
	r.Register(&App{URI: "ui://a/b", ServerID: "a", Name: "newer", DiscoveredAt: time.Now().UTC()})
	got, _ := r.Lookup("ui://a/b")
	if got.Name != "newer" {
		t.Errorf("expected upsert to overwrite Name, got %q", got.Name)
	}
}

func TestRegistry_ListByServerAndForget(t *testing.T) {
	r := New(CSPConfig{})
	r.Register(&App{URI: "ui://a/x", ServerID: "a"})
	r.Register(&App{URI: "ui://a/y", ServerID: "a"})
	r.Register(&App{URI: "ui://b/x", ServerID: "b"})
	if got := r.ListByServer("a"); len(got) != 2 {
		t.Errorf("ListByServer(a) = %d", len(got))
	}
	if removed := r.Forget("a"); removed != 2 {
		t.Errorf("Forget(a) removed %d", removed)
	}
	if got := r.ListByServer("a"); len(got) != 0 {
		t.Errorf("ListByServer(a) post-forget = %d", len(got))
	}
	if got := r.ListByServer("b"); len(got) != 1 {
		t.Errorf("server b should still have 1 app; got %d", len(got))
	}
}

func TestCSP_HeaderAllDirectives(t *testing.T) {
	c := DefaultCSP()
	h := c.Header()
	for _, want := range []string{"default-src 'self'", "script-src 'self'", "style-src 'self'", "img-src 'self' data:"} {
		if !strings.Contains(h, want) {
			t.Errorf("header missing %q: %s", want, h)
		}
	}
}

func TestCSP_Compose_HasHead_InjectsMeta(t *testing.T) {
	in := []byte(`<!doctype html><html><head><title>x</title></head><body>hi</body></html>`)
	out, meta := DefaultCSP().Compose(in)
	if !strings.Contains(string(out), `http-equiv="Content-Security-Policy"`) {
		t.Errorf("CSP meta not injected: %s", out)
	}
	if !strings.Contains(string(out), `<title>x</title>`) {
		t.Errorf("original head content missing: %s", out)
	}
	if meta["csp"] == "" || meta["sandbox"] == "" {
		t.Errorf("meta map missing entries: %+v", meta)
	}
}

func TestCSP_Compose_NoHead_CreatesHead(t *testing.T) {
	in := []byte(`<html><body>hi</body></html>`)
	out, _ := DefaultCSP().Compose(in)
	if !strings.Contains(string(out), `<head>`) {
		t.Errorf("head not synthesised: %s", out)
	}
	if !strings.Contains(string(out), `Content-Security-Policy`) {
		t.Errorf("CSP meta missing after synthesising head")
	}
}

func TestCSP_Compose_PassesThroughOnFailure(t *testing.T) {
	// Even garbage HTML survives — html.Parse is very forgiving — so the
	// real assertion is that we always emit the meta map.
	in := []byte("not html at all")
	_, meta := DefaultCSP().Compose(in)
	if meta["csp"] == "" {
		t.Errorf("meta map should be populated even when wrapping no-ops")
	}
}

func TestCSP_WithDefaults_FillsEmpty(t *testing.T) {
	cfg := CSPConfig{ScriptSrc: []string{"'self'", "'unsafe-inline'"}}.WithDefaults()
	if len(cfg.DefaultSrc) == 0 {
		t.Errorf("DefaultSrc not filled")
	}
	if cfg.Sandbox == "" {
		t.Errorf("Sandbox not filled")
	}
	if !strings.Contains(strings.Join(cfg.ScriptSrc, " "), "'unsafe-inline'") {
		t.Errorf("operator override on ScriptSrc lost: %v", cfg.ScriptSrc)
	}
}
