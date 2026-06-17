// Package catalog provides tests for the JSON Schema to Python .pyi stub translator.
package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSchemaToPyType(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		expected string
	}{
		{"string", `{"type":"string"}`, "str"},
		{"integer", `{"type":"integer"}`, "int"},
		{"number", `{"type":"number"}`, "float"},
		{"boolean", `{"type":"boolean"}`, "bool"},
		{"object", `{"type":"object"}`, "dict"},
		{"null", `{"type":"null"}`, "None"},
		{"array of string", `{"type":"array","items":{"type":"string"}}`, "list[str]"},
		{"array no items", `{"type":"array"}`, "list"},
		{"union with null", `{"type":["string","null"]}`, "str"},
		{"union null first", `{"type":["null","integer"]}`, "int"},
		{"all null union", `{"type":["null","null"]}`, "Any"},
		{"empty union", `{"type":[]}`, "Any"},
		{"enum string", `{"enum":["a","b","c"]}`, "str"},
		{"enum int", `{"enum":[1,2,3]}`, "int"},
		{"enum float", `{"enum":[1.5,2.5]}`, "float"},
		{"enum bool", `{"enum":[true,false]}`, "bool"},
		{"missing type", `{}`, "Any"},
		{"unknown type", `{"type":"unknown"}`, "Any"},
		{"malformed json", `not-json`, "Any"},
		{"nested array", `{"type":"array","items":{"type":"array","items":{"type":"string"}}}`, "list[list[str]]"},
		{"array of object", `{"type":"array","items":{"type":"object"}}`, "list[dict]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := schemaToPyType(json.RawMessage(tt.schema))
			if result != tt.expected {
				t.Errorf("schemaToPyType(%s) = %q, want %q", tt.schema, result, tt.expected)
			}
		})
	}
}

func TestToolStub(t *testing.T) {
	t.Run("required and optional params", func(t *testing.T) {
		schema := `{
			"type": "object",
			"properties": {
				"repo": {"type": "string"},
				"state": {"type": "string"},
				"per_page": {"type": "integer"}
			},
			"required": ["repo", "state"]
		}`
		result := ToolStub("github.list_issues", "List issues for a repo", json.RawMessage(schema))
		expected := `def github_list_issues(repo: str, state: str, per_page: int = None) -> dict:
    """List issues for a repo"""
    ...`
		if result != expected {
			t.Errorf("ToolStub with required+optional:\ngot:\n%s\nwant:\n%s", result, expected)
		}
	})

	t.Run("empty input schema", func(t *testing.T) {
		result := ToolStub("test_tool", "A test tool", json.RawMessage(`{}`))
		expected := `def test_tool() -> dict:
    """A test tool"""
    ...`
		if result != expected {
			t.Errorf("ToolStub with empty schema:\ngot:\n%s\nwant:\n%s", result, expected)
		}
	})

	t.Run("name sanitization", func(t *testing.T) {
		result := ToolStub("github.list-issues", "Desc", json.RawMessage(`{}`))
		if !strings.HasPrefix(result, "def github_list_issues(") {
			t.Errorf("sanitized name: got %q, want prefix github_list_issues", result)
		}
	})

	t.Run("description docstring present", func(t *testing.T) {
		result := ToolStub("my_tool", "First line\nSecond line", json.RawMessage(`{}`))
		if !strings.Contains(result, `"""First line"""`) {
			t.Errorf("docstring missing or wrong: got %q", result)
		}
	})

	t.Run("description docstring omitted when empty", func(t *testing.T) {
		result := ToolStub("my_tool", "", json.RawMessage(`{}`))
		if strings.Contains(result, `"""`) {
			t.Errorf("docstring should be omitted for empty description: got %q", result)
		}
		if !strings.HasSuffix(result, "-> dict:\n    ...") {
			t.Errorf("wrong suffix for no-docstring: got %q", result)
		}
	})

	t.Run("nested array of object", func(t *testing.T) {
		schema := `{
			"type": "object",
			"properties": {
				"items": {"type": "array", "items": {"type": "object"}}
			},
			"required": ["items"]
		}`
		result := ToolStub("test", "Desc", json.RawMessage(schema))
		if !strings.Contains(result, "items: list[dict]") {
			t.Errorf("nested array of object: got %q", result)
		}
	})

	t.Run("optional params sorted", func(t *testing.T) {
		schema := `{
			"type": "object",
			"properties": {
				"zebra": {"type": "string"},
				"alpha": {"type": "string"}
			},
			"required": []
		}`
		result := ToolStub("test", "Desc", json.RawMessage(schema))
		// alpha should come before zebra
		alphaIdx := strings.Index(result, "alpha")
		zebraIdx := strings.Index(result, "zebra")
		if alphaIdx == -1 || zebraIdx == -1 || alphaIdx > zebraIdx {
			t.Errorf("optional params not sorted: got %q", result)
		}
	})
}

func TestSanitizePyName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"github.list-issues", "github_list_issues"},
		{"123start", "_123start"},
		{"", "_"},
		{"with spaces", "with_spaces"},
		{"special!@#$chars", "special____chars"},
		{"already_valid_name", "already_valid_name"},
		{"UPPER_CASE", "UPPER_CASE"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizePyName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizePyName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
