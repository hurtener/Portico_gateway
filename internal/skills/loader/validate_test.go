package loader

import (
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
)

const validManifest = `id: acme.test
title: Test
version: 1.0.0
spec: skills/v1
instructions: SKILL.md
binding:
  required_tools:
    - acme.do
`

const missingRequiredManifest = `id: acme.test
version: 1.0.0
spec: skills/v1
binding: {}
`

func mustSchema(t *testing.T) *manifest.Schema {
	t.Helper()
	s, err := manifest.CompileSchema()
	if err != nil {
		t.Fatalf("CompileSchema: %v", err)
	}
	return s
}

func TestValidateManifestBytes_Valid(t *testing.T) {
	res := ValidateManifestBytes([]byte(validManifest), mustSchema(t))
	if res.HasErrors() {
		t.Errorf("expected no violations, got %+v", res.Violations)
	}
}

func TestValidateManifestBytes_MissingTitle_HasJSONPointer(t *testing.T) {
	res := ValidateManifestBytes([]byte(missingRequiredManifest), mustSchema(t))
	if !res.HasErrors() {
		t.Fatal("expected violations")
	}
	for _, v := range res.Violations {
		if v.Reason == "" {
			t.Errorf("violation missing reason: %+v", v)
		}
	}
}

func TestValidateManifestBytes_BrokenYAML_PointsToTheError(t *testing.T) {
	body := `id: acme.test
title: Test
version: 1.0.0
spec: skills/v1
instructions: SKILL.md
binding:
  required_tools: 42
`
	res := ValidateManifestBytes([]byte(body), mustSchema(t))
	if !res.HasErrors() {
		t.Fatal("expected violations")
	}
	found := false
	for _, v := range res.Violations {
		if strings.Contains(v.Pointer, "required_tools") || strings.Contains(v.Reason, "required_tools") {
			found = true
		}
	}
	if !found {
		t.Errorf("violation pointer should mention required_tools: %+v", res.Violations)
	}
}
