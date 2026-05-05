package namespace_test

import (
	"testing"

	"github.com/hurtener/Portico_gateway/internal/catalog/namespace"
)

func TestJoinSplit_Roundtrip(t *testing.T) {
	cases := []struct{ server, tool string }{
		{"github", "get_pull_request"},
		{"postgres", "run_sql"},
		{"linear", "list_issues"},
	}
	for _, c := range cases {
		q := namespace.JoinTool(c.server, c.tool)
		s, n, ok := namespace.SplitTool(q)
		if !ok {
			t.Errorf("%q: split failed", q)
		}
		if s != c.server || n != c.tool {
			t.Errorf("%q: split = (%q, %q) want (%q, %q)", q, s, n, c.server, c.tool)
		}
	}
}

func TestSplit_NoSeparator_ReturnsFalse(t *testing.T) {
	if _, _, ok := namespace.SplitTool("nodot"); ok {
		t.Error("expected ok=false for input without '.'")
	}
	if _, _, ok := namespace.SplitTool("trailing."); ok {
		t.Error("expected ok=false when nothing after '.'")
	}
	if _, _, ok := namespace.SplitTool(".leading"); ok {
		t.Error("expected ok=false when nothing before '.'")
	}
}

func TestSplit_DotInToolName(t *testing.T) {
	s, n, ok := namespace.SplitTool("github.foo.bar")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if s != "github" || n != "foo.bar" {
		t.Errorf("got (%q, %q) want (github, foo.bar)", s, n)
	}
}

func TestValidateServerID(t *testing.T) {
	good := []string{"github", "g", "ab-1", "x_y_z", "0abc"}
	for _, id := range good {
		if err := namespace.ValidateServerID(id); err != nil {
			t.Errorf("%q should be valid: %v", id, err)
		}
	}
	bad := []string{"", "Github", "git hub", "_leading", "-leading", "with.dot", strings(33)}
	for _, id := range bad {
		if err := namespace.ValidateServerID(id); err == nil {
			t.Errorf("%q should be invalid", id)
		}
	}
}

// strings(n) returns a string of n 'a's; used to test length cap.
func strings(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
