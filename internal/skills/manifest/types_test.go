package manifest

import (
	"strings"
	"testing"
)

const validManifest = `
id: github.code-review
title: GitHub Code Review
version: 0.1.0
spec: skills/v1
description: Review PRs.
instructions: ./SKILL.md
resources:
  - resources/guide.md
prompts:
  - prompts/review_pr.md
binding:
  server_dependencies: [github]
  required_tools:
    - github.get_pull_request
  optional_tools:
    - github.create_review_comment
  policy:
    requires_approval:
      - github.create_review_comment
    risk_classes:
      github.create_review_comment: external_side_effect
  ui:
    resource_uri: ui://github/code-review-panel.html
  entitlements:
    plans: [pro, enterprise]
`

func TestParse_Valid(t *testing.T) {
	m, _, err := Parse([]byte(validManifest))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "github.code-review" {
		t.Errorf("id = %q", m.ID)
	}
	if m.Spec != SpecVersion {
		t.Errorf("spec = %q", m.Spec)
	}
	if m.Binding.UI == nil || m.Binding.UI.ResourceURI != "ui://github/code-review-panel.html" {
		t.Errorf("ui binding lost: %+v", m.Binding.UI)
	}
	if got := m.Binding.Policy.RiskClasses["github.create_review_comment"]; got != "external_side_effect" {
		t.Errorf("risk class = %q", got)
	}
}

func TestParse_BadYAML(t *testing.T) {
	_, _, err := Parse([]byte("not: [valid yaml"))
	if err == nil {
		t.Fatalf("expected yaml error")
	}
}

func TestSchema_Compiles(t *testing.T) {
	if _, err := CompileSchema(); err != nil {
		t.Fatalf("CompileSchema: %v", err)
	}
}

func TestSchema_ValidatesGoodManifest(t *testing.T) {
	s, err := CompileSchema()
	if err != nil {
		t.Fatal(err)
	}
	_, doc, err := Parse([]byte(validManifest))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(doc); err != nil {
		t.Fatalf("good manifest rejected: %v", err)
	}
}

func TestSchema_RejectsBadID(t *testing.T) {
	s, _ := CompileSchema()
	bad := strings.Replace(validManifest, "id: github.code-review", "id: GITHUB", 1)
	_, doc, _ := Parse([]byte(bad))
	if err := s.Validate(doc); err == nil {
		t.Errorf("expected schema rejection for uppercase id")
	}
}

func TestSchema_RejectsBadSpec(t *testing.T) {
	s, _ := CompileSchema()
	bad := strings.Replace(validManifest, "spec: skills/v1", "spec: skills/v2", 1)
	_, doc, _ := Parse([]byte(bad))
	if err := s.Validate(doc); err == nil {
		t.Errorf("expected schema rejection for unknown spec")
	}
}

func TestSchema_RejectsAbsoluteInstructions(t *testing.T) {
	s, _ := CompileSchema()
	bad := strings.Replace(validManifest, "instructions: ./SKILL.md", "instructions: /etc/passwd", 1)
	_, doc, _ := Parse([]byte(bad))
	if err := s.Validate(doc); err == nil {
		t.Errorf("expected schema rejection for absolute path")
	}
}

func TestSchema_RejectsBadRiskClass(t *testing.T) {
	s, _ := CompileSchema()
	bad := strings.Replace(validManifest,
		"github.create_review_comment: external_side_effect",
		"github.create_review_comment: chaos", 1)
	_, doc, _ := Parse([]byte(bad))
	if err := s.Validate(doc); err == nil {
		t.Errorf("expected schema rejection for unknown risk class")
	}
}

func TestSchema_RejectsBadUIURI(t *testing.T) {
	s, _ := CompileSchema()
	bad := strings.Replace(validManifest,
		"resource_uri: ui://github/code-review-panel.html",
		"resource_uri: file:///etc/passwd", 1)
	_, doc, _ := Parse([]byte(bad))
	if err := s.Validate(doc); err == nil {
		t.Errorf("expected schema rejection for non-ui:// resource_uri")
	}
}

func TestManifest_AllTools(t *testing.T) {
	m := &Manifest{Binding: Binding{
		RequiredTools: []string{"a.x", "a.y"},
		OptionalTools: []string{"b.z"},
	}}
	got := m.AllTools()
	if len(got) != 3 || got[0] != "a.x" || got[2] != "b.z" {
		t.Errorf("AllTools = %v", got)
	}
}
