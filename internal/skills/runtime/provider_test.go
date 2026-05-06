package runtime

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/skills/source"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

const testManifest = `id: github.code-review
title: GitHub Code Review
version: 0.1.0
spec: skills/v1
description: Review PRs.
instructions: SKILL.md
resources:
  - resources/guide.md
prompts:
  - prompts/review_pr.md
binding:
  required_tools:
    - github.get_pull_request
`

func writeTestPack(t *testing.T) (string, *Skill) {
	t.Helper()
	root := t.TempDir()
	pack := filepath.Join(root, "github", "code-review")
	for _, sub := range []string{"resources", "prompts"} {
		if err := os.MkdirAll(filepath.Join(pack, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"manifest.yaml":        testManifest,
		"SKILL.md":             "# How to use\nFollow the prompts.\n",
		"resources/guide.md":   "# Guide\n",
		"prompts/review_pr.md": "---\nname: review_pr\ndescription: Review PR template.\narguments:\n  - name: owner\n    required: true\n---\nReview PR for {{.owner}}.\n",
	}
	for rel, body := range files {
		path := filepath.Join(pack, rel)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	src, err := source.NewLocalDir(root, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	refs, _ := src.List(context.Background())
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref; got %d", len(refs))
	}
	m, err := src.Open(context.Background(), refs[0])
	if err != nil {
		t.Fatal(err)
	}
	manifestCopy := m
	return root, &Skill{
		Manifest: &manifestCopy,
		Source:   src,
		Ref:      refs[0],
	}
}

func TestProvider_ListResources_IncludesSkillFiles(t *testing.T) {
	_, skill := writeTestPack(t)
	cat := NewCatalog()
	cat.Set(skill)

	en := NewEnablement(nil, ModeAuto) // store nil → auto means enabled
	idx := NewIndexGenerator(cat, en, nil, nil)
	prov := NewSkillProvider(cat, en, idx)

	got, err := prov.ListResources(context.Background(), "acme", "s1", "", []string{"*"})
	if err != nil {
		t.Fatal(err)
	}
	uris := map[string]bool{}
	for _, r := range got {
		uris[r.URI] = true
	}
	wantURIs := []string{
		"skill://_index",
		"skill://github/code-review/manifest.yaml",
		"skill://github/code-review/SKILL.md",
		"skill://github/code-review/resources/guide.md",
		"skill://github/code-review/prompts/review_pr.md",
	}
	for _, u := range wantURIs {
		if !uris[u] {
			t.Errorf("missing URI: %q (got %v)", u, uris)
		}
	}
}

func TestProvider_ReadResource_Manifest(t *testing.T) {
	_, skill := writeTestPack(t)
	cat := NewCatalog()
	cat.Set(skill)
	en := NewEnablement(nil, ModeAuto)
	prov := NewSkillProvider(cat, en, NewIndexGenerator(cat, en, nil, nil))

	res, err := prov.ReadResource(context.Background(), "acme", "s1", "skill://github/code-review/manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Contents[0].Text, "github.code-review") {
		t.Errorf("manifest body missing id: %q", res.Contents[0].Text)
	}
}

func TestProvider_ReadResource_SkillMD(t *testing.T) {
	_, skill := writeTestPack(t)
	cat := NewCatalog()
	cat.Set(skill)
	en := NewEnablement(nil, ModeAuto)
	prov := NewSkillProvider(cat, en, NewIndexGenerator(cat, en, nil, nil))

	res, err := prov.ReadResource(context.Background(), "acme", "s1", "skill://github/code-review/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Contents[0].Text, "Follow the prompts") {
		t.Errorf("SKILL.md missing")
	}
}

func TestProvider_ReadResource_Index(t *testing.T) {
	_, skill := writeTestPack(t)
	cat := NewCatalog()
	cat.Set(skill)
	en := NewEnablement(nil, ModeAuto)
	idx := NewIndexGenerator(cat, en, func(_ string) (string, []string) { return "free", []string{"*"} }, nil)
	prov := NewSkillProvider(cat, en, idx)

	res, err := prov.ReadResource(context.Background(), "acme", "s1", "skill://_index")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version int `json:"version"`
		Skills  []struct {
			ID                string `json:"id"`
			EnabledForTenant  bool   `json:"enabled_for_tenant"`
			EnabledForSession bool   `json:"enabled_for_session"`
		} `json:"skills"`
	}
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &doc); err != nil {
		t.Fatalf("index JSON: %v", err)
	}
	if doc.Version != 1 {
		t.Errorf("version = %d", doc.Version)
	}
	if len(doc.Skills) != 1 || doc.Skills[0].ID != "github.code-review" {
		t.Errorf("skills slice wrong: %+v", doc.Skills)
	}
}

func TestProvider_ListPrompts_NamesPrefixed(t *testing.T) {
	_, skill := writeTestPack(t)
	cat := NewCatalog()
	cat.Set(skill)
	en := NewEnablement(nil, ModeAuto)
	prov := NewSkillProvider(cat, en, NewIndexGenerator(cat, en, nil, nil))

	got, err := prov.ListPrompts(context.Background(), "acme", "s1", "", []string{"*"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "github.code-review.review_pr" {
		t.Fatalf("prompts = %+v", got)
	}
	if got[0].Description != "Review PR template." {
		t.Errorf("description = %q", got[0].Description)
	}
}

func TestProvider_GetPrompt_RendersWithArgs(t *testing.T) {
	_, skill := writeTestPack(t)
	cat := NewCatalog()
	cat.Set(skill)
	en := NewEnablement(nil, ModeAuto)
	prov := NewSkillProvider(cat, en, NewIndexGenerator(cat, en, nil, nil))

	res, err := prov.GetPrompt(context.Background(), "acme", "s1", "github.code-review.review_pr", map[string]string{"owner": "anthropic"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Messages[0].Content.Text, "anthropic") {
		t.Errorf("template not substituted: %q", res.Messages[0].Content.Text)
	}
}

func TestProvider_GetPrompt_RejectsBareName(t *testing.T) {
	cat := NewCatalog()
	en := NewEnablement(nil, ModeAuto)
	prov := NewSkillProvider(cat, en, NewIndexGenerator(cat, en, nil, nil))
	if _, err := prov.GetPrompt(context.Background(), "acme", "s1", "noskill.prompt", nil); err == nil {
		t.Errorf("expected error for unknown skill")
	}
}

func TestSplitSkillURI(t *testing.T) {
	cases := []struct {
		in            string
		ns, name, rel string
		ok            bool
	}{
		{"skill://github/code-review/SKILL.md", "github", "code-review", "SKILL.md", true},
		{"skill://github/code-review/prompts/x.md", "github", "code-review", "prompts/x.md", true},
		{"skill://only-one", "", "", "", false},
		{"file:///etc/passwd", "", "", "", false},
	}
	for _, c := range cases {
		ns, name, rel, ok := splitSkillURI(c.in)
		if ok != c.ok || ns != c.ns || name != c.name || rel != c.rel {
			t.Errorf("splitSkillURI(%q) = (%q,%q,%q,%v); want (%q,%q,%q,%v)",
				c.in, ns, name, rel, ok, c.ns, c.name, c.rel, c.ok)
		}
	}
}
