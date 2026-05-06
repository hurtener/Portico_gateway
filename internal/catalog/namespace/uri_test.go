package namespace

import "testing"

func TestRewriteRestore_FileURI(t *testing.T) {
	rew, sandbox := RewriteResourceURI("github", "file:///etc/hosts")
	if sandbox {
		t.Errorf("file:// should not set sandbox=true")
	}
	if rew != "mcp+server://github/file/etc/hosts" {
		t.Errorf("rewrite = %q", rew)
	}
	srv, orig, ui, ok := RestoreResourceURI(rew)
	if !ok || ui {
		t.Fatalf("restore: ok=%v ui=%v", ok, ui)
	}
	if srv != "github" || orig != "file:///etc/hosts" {
		t.Errorf("restore = (%q, %q)", srv, orig)
	}
}

func TestRewriteRestore_HTTPSURI(t *testing.T) {
	rew, _ := RewriteResourceURI("docs", "https://example.com/path/to/page.md")
	if rew != "mcp+server://docs/https/example.com/path/to/page.md" {
		t.Fatalf("rewrite = %q", rew)
	}
	srv, orig, _, ok := RestoreResourceURI(rew)
	if !ok || srv != "docs" || orig != "https://example.com/path/to/page.md" {
		t.Errorf("restore = (%q, %q, ok=%v)", srv, orig, ok)
	}
}

func TestRewriteRestore_UIURI(t *testing.T) {
	rew, sandbox := RewriteResourceURI("github", "ui://code-review-panel.html")
	if !sandbox {
		t.Errorf("ui:// must set sandbox=true")
	}
	if rew != "ui://github/code-review-panel.html" {
		t.Errorf("rewrite = %q", rew)
	}
	srv, orig, ui, ok := RestoreResourceURI(rew)
	if !ok || !ui || srv != "github" || orig != "ui://code-review-panel.html" {
		t.Errorf("restore = (%q, %q, ui=%v, ok=%v)", srv, orig, ui, ok)
	}
}

func TestRewriteRestore_CustomScheme(t *testing.T) {
	original := "custom-scheme://opaque/blob?x=1#frag"
	rew, _ := RewriteResourceURI("svc", original)
	srv, orig, _, ok := RestoreResourceURI(rew)
	if !ok || srv != "svc" || orig != original {
		t.Errorf("roundtrip lost data: rew=%q -> (%q, %q)", rew, srv, orig)
	}
}

func TestRewriteResource_Idempotent(t *testing.T) {
	rew1, _ := RewriteResourceURI("svc", "https://x/y")
	rew2, _ := RewriteResourceURI("svc", rew1)
	if rew1 != rew2 {
		t.Errorf("rewrite not idempotent: %q -> %q", rew1, rew2)
	}
}

func TestRewriteResource_UIIdempotent(t *testing.T) {
	rew1, _ := RewriteResourceURI("github", "ui://x.html")
	rew2, sandbox := RewriteResourceURI("github", rew1)
	if rew1 != rew2 || !sandbox {
		t.Errorf("ui rewrite not idempotent: %q -> %q sandbox=%v", rew1, rew2, sandbox)
	}
}

func TestRestoreResource_RejectsUnnamespaced(t *testing.T) {
	cases := []string{"", "https://x", "file:///etc/hosts", "mcp+server://onlysrv"}
	for _, c := range cases {
		if _, _, _, ok := RestoreResourceURI(c); ok {
			t.Errorf("unexpectedly accepted %q", c)
		}
	}
}

func TestRewriteRestorePromptName(t *testing.T) {
	rew := RewritePromptName("github", "code.review")
	if rew != "github.code.review" {
		t.Fatalf("rewrite = %q", rew)
	}
	srv, orig, ok := RestorePromptName(rew)
	if !ok || srv != "github" || orig != "code.review" {
		t.Errorf("restore = (%q, %q, ok=%v)", srv, orig, ok)
	}
}

func TestRestorePromptName_RejectsBare(t *testing.T) {
	for _, c := range []string{"", "noprefix", ".leading", "trailing."} {
		if _, _, ok := RestorePromptName(c); ok {
			t.Errorf("unexpectedly accepted %q", c)
		}
	}
}
