package mcpgw

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/profiles"
)

func TestSkillIDFromResourceURI(t *testing.T) {
	cases := []struct {
		uri    string
		wantID string
		wantOK bool
	}{
		{"skill://github/code-review/manifest.yaml", "github.code-review", true},
		{"skill://github/code-review/prompts/review.md", "github.code-review", true},
		{"skill://_index", "", false},
		{"skill://github", "", false},
		{"mcp+server://github/x", "", false},
	}
	for _, c := range cases {
		id, ok := skillIDFromResourceURI(c.uri)
		if id != c.wantID || ok != c.wantOK {
			t.Errorf("skillIDFromResourceURI(%q) = (%q,%v), want (%q,%v)", c.uri, id, ok, c.wantID, c.wantOK)
		}
	}
}

func TestSkillIDFromPromptName(t *testing.T) {
	cases := []struct {
		name   string
		wantID string
		wantOK bool
	}{
		{"github.code-review.review_pr", "github.code-review", true},
		{"acme.triage.summarize", "acme.triage", true},
		{"github.list_issues", "", false}, // a plain server.prompt, one dot
		{"bare", "", false},
	}
	for _, c := range cases {
		id, ok := skillIDFromPromptName(c.name)
		if id != c.wantID || ok != c.wantOK {
			t.Errorf("skillIDFromPromptName(%q) = (%q,%v), want (%q,%v)", c.name, id, ok, c.wantID, c.wantOK)
		}
	}
}

func TestFilterSkillPromptsByProfile(t *testing.T) {
	prompts := []protocol.Prompt{
		{Name: "github.code-review.review_pr"},
		{Name: "acme.triage.summarize"},
	}
	// Profile allows only code-review.
	prof := &profiles.Profile{AllowedSkills: []string{"github.code-review"}}
	got := filterSkillPromptsByProfile(profiles.WithProfile(context.Background(), prof), prompts)
	if len(got) != 1 || got[0].Name != "github.code-review.review_pr" {
		t.Fatalf("restrictive prompt filter wrong: %+v", got)
	}
	// Default profile → unchanged.
	if g := filterSkillPromptsByProfile(profiles.WithProfile(context.Background(), profiles.DefaultProfile("t1")), prompts); len(g) != 2 {
		t.Errorf("default profile must not filter prompts: got %d", len(g))
	}
	// Absent profile → unchanged.
	if g := filterSkillPromptsByProfile(context.Background(), prompts); len(g) != 2 {
		t.Errorf("absent profile must not filter prompts: got %d", len(g))
	}
}

func TestFilterSkillResourcesByProfile(t *testing.T) {
	res := []protocol.Resource{
		{URI: "skill://_index"},
		{URI: "skill://github/code-review/manifest.yaml"},
		{URI: "skill://acme/triage/manifest.yaml"},
	}
	prof := &profiles.Profile{AllowedSkills: []string{"github.code-review"}}
	got := filterSkillResourcesByProfile(profiles.WithProfile(context.Background(), prof), res)
	// _index retained + only the allowed skill's resource.
	if len(got) != 2 {
		t.Fatalf("restrictive resource filter wrong count: %+v", got)
	}
	var sawIndex, sawAcme bool
	for _, r := range got {
		if r.URI == "skill://_index" {
			sawIndex = true
		}
		if r.URI == "skill://acme/triage/manifest.yaml" {
			sawAcme = true
		}
	}
	if !sawIndex {
		t.Error("_index must be retained")
	}
	if sawAcme {
		t.Error("disallowed skill resource must be dropped")
	}
	// Default profile → unchanged.
	if g := filterSkillResourcesByProfile(profiles.WithProfile(context.Background(), profiles.DefaultProfile("t1")), res); len(g) != 3 {
		t.Errorf("default profile must not filter resources: got %d", len(g))
	}
}

func TestFilterSkillIndexBody(t *testing.T) {
	body := []byte(`{"version":1,"tenant_id":"t1","skills":[{"id":"github.code-review","title":"x"},{"id":"acme.triage","title":"y"}]}`)
	prof := &profiles.Profile{AllowedSkills: []string{"github.code-review"}}
	out := filterSkillIndexBody(profiles.WithProfile(context.Background(), prof), body)

	var doc struct {
		Skills []struct {
			ID string `json:"id"`
		} `json:"skills"`
		TenantID string `json:"tenant_id"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Skills) != 1 || doc.Skills[0].ID != "github.code-review" {
		t.Fatalf("index body not filtered to allowed skills: %s", out)
	}
	if doc.TenantID != "t1" {
		t.Errorf("index body lost sibling fields: %s", out)
	}
	// Default profile → unchanged bytes.
	if g := filterSkillIndexBody(profiles.WithProfile(context.Background(), profiles.DefaultProfile("t1")), body); string(g) != string(body) {
		t.Error("default profile must not rewrite the index body")
	}
	// Malformed body → returned untouched (best-effort).
	bad := []byte(`not json`)
	if g := filterSkillIndexBody(profiles.WithProfile(context.Background(), prof), bad); string(g) != string(bad) {
		t.Error("malformed index body must be returned untouched")
	}
}
