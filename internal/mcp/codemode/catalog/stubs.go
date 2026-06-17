// Package catalog provides the JSON Schema to Python .pyi stub translator
// for Code Mode's virtual catalog.
package catalog

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// schemaToPyType converts a JSON Schema fragment to a Python type annotation.
// It handles the type mapping table from the Phase 13.5 specification.
func schemaToPyType(raw json.RawMessage) string {
	var schema map[string]json.RawMessage
	if err := json.Unmarshal(raw, &schema); err != nil {
		return "Any"
	}

	// Check for enum first (no type but has enum)
	if enumRaw, ok := schema["enum"]; ok {
		var enumVals []json.RawMessage
		if err := json.Unmarshal(enumRaw, &enumVals); err == nil && len(enumVals) > 0 {
			return inferTypeFromEnumValue(enumVals[0])
		}
		return "Any"
	}

	// Handle type field
	typeRaw, hasType := schema["type"]
	if !hasType {
		return "Any"
	}

	// Type can be a string or an array (union)
	var typeStr string
	var typeArr []string

	if err := json.Unmarshal(typeRaw, &typeStr); err == nil {
		// Single type string
		return pyTypeFromString(typeStr, schema)
	}

	if err := json.Unmarshal(typeRaw, &typeArr); err == nil {
		// Union type array - find first non-null
		for _, t := range typeArr {
			if t != "null" {
				return pyTypeFromString(t, schema)
			}
		}
		return "Any"
	}

	return "Any"
}

// pyTypeFromString maps a single JSON Schema type string to Python annotation.
func pyTypeFromString(t string, schema map[string]json.RawMessage) string {
	switch t {
	case "string":
		return "str"
	case "integer":
		return "int"
	case "number":
		return "float"
	case "boolean":
		return "bool"
	case "object":
		return "dict"
	case "null":
		return "None"
	case "array":
		// Check for items
		if itemsRaw, ok := schema["items"]; ok {
			itemType := schemaToPyType(itemsRaw)
			return fmt.Sprintf("list[%s]", itemType)
		}
		return "list"
	default:
		return "Any"
	}
}

// inferTypeFromEnumValue infers Python type from the first enum value's JSON kind.
func inferTypeFromEnumValue(val json.RawMessage) string {
	var v any
	if err := json.Unmarshal(val, &v); err != nil {
		return "Any"
	}
	switch x := v.(type) {
	case string:
		return "str"
	case float64:
		// Integral JSON numbers (1, 2, 3) read as int; fractional as float.
		if x == float64(int64(x)) {
			return "int"
		}
		return "float"
	case bool:
		return "bool"
	default:
		return "Any"
	}
}

// sanitizePyName converts a string to a valid Python identifier.
// Replaces every rune that is not [A-Za-z0-9_] with _.
// If the result is empty or starts with a digit, prefixes with _.
func sanitizePyName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	s := b.String()
	if s == "" || (len(s) > 0 && s[0] >= '0' && s[0] <= '9') {
		s = "_" + s
	}
	return s
}

// ToolStub renders one MCP tool as a Python .pyi function signature with a
// docstring, for the Code Mode virtual catalog.
func ToolStub(name, description string, inputSchema json.RawMessage) string {
	pyName := sanitizePyName(name)

	// Parse input schema for parameters
	var params []string
	if len(inputSchema) > 0 && string(inputSchema) != "null" {
		var schema map[string]json.RawMessage
		if err := json.Unmarshal(inputSchema, &schema); err == nil {
			if propsRaw, ok := schema["properties"]; ok {
				var props map[string]json.RawMessage
				if err := json.Unmarshal(propsRaw, &props); err == nil {
					// Get required fields
					var required []string
					if reqRaw, ok := schema["required"]; ok {
						_ = json.Unmarshal(reqRaw, &required)
					}
					requiredSet := make(map[string]bool, len(required))
					for _, r := range required {
						requiredSet[r] = true
					}

					// Required params first (in required[] order)
					for _, reqName := range required {
						if propSchema, ok := props[reqName]; ok {
							pyParamName := sanitizePyName(reqName)
							pyType := schemaToPyType(propSchema)
							params = append(params, fmt.Sprintf("%s: %s", pyParamName, pyType))
						}
					}

					// Optional params (sorted keys for determinism)
					var optionalNames []string
					for propName := range props {
						if !requiredSet[propName] {
							optionalNames = append(optionalNames, propName)
						}
					}
					sort.Strings(optionalNames)
					for _, optName := range optionalNames {
						propSchema := props[optName]
						pyParamName := sanitizePyName(optName)
						pyType := schemaToPyType(propSchema)
						params = append(params, fmt.Sprintf("%s: %s = None", pyParamName, pyType))
					}
				}
			}
		}
	}

	// Build signature
	sig := fmt.Sprintf("def %s(%s) -> dict:", pyName, strings.Join(params, ", "))

	// Build docstring if description is non-empty
	var docstring string
	if strings.TrimSpace(description) != "" {
		firstLine := strings.Split(description, "\n")[0]
		firstLine = strings.TrimSpace(firstLine)
		// Escape double quotes
		firstLine = strings.ReplaceAll(firstLine, `"`, `\"`)
		docstring = fmt.Sprintf("\n    \"\"\"%s\"\"\"", firstLine)
	}

	return sig + docstring + "\n    ..."
}
