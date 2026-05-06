package manifest

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Parse returns the typed Manifest plus the canonicalised JSON form
// (suitable for passing into Schema.Validate). Splitting the two lets
// callers schema-validate AND surface yaml line/column on parse errors
// without re-walking the YAML tree.
func Parse(body []byte) (*Manifest, any, error) {
	var raw any
	if err := yaml.Unmarshal(body, &raw); err != nil {
		return nil, nil, fmt.Errorf("yaml parse: %w", err)
	}
	// Round-trip through JSON so map keys are strings and types are
	// JSON-Schema-compatible (yaml.v3 hands back map[any]any in some
	// cases which JSON Schema doesn't speak).
	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("yaml→json: %w", err)
	}
	var jsonDoc any
	if err := json.Unmarshal(jsonBytes, &jsonDoc); err != nil {
		return nil, nil, fmt.Errorf("json reparse: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		return nil, nil, fmt.Errorf("manifest decode: %w", err)
	}
	return &m, jsonDoc, nil
}
