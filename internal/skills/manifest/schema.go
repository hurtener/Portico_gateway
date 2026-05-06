package manifest

import (
	"embed"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schema.json
var schemaFS embed.FS

// Schema wraps a compiled JSON Schema instance. Built once at startup
// via CompileSchema; reused across every manifest validation.
type Schema struct {
	compiled *jsonschema.Schema
}

// CompileSchema loads the embedded skills/v1 schema. Returns an error
// only if the embedded JSON is malformed, which would be a build-time
// regression.
func CompileSchema() (*Schema, error) {
	body, err := schemaFS.ReadFile("schema.json")
	if err != nil {
		return nil, fmt.Errorf("schema.json: %w", err)
	}
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("schema.json parse: %w", err)
	}
	c := jsonschema.NewCompiler()
	const id = "https://portico.dev/schema/skills/v1.json"
	if err := c.AddResource(id, doc); err != nil {
		return nil, fmt.Errorf("schema add: %w", err)
	}
	s, err := c.Compile(id)
	if err != nil {
		return nil, fmt.Errorf("schema compile: %w", err)
	}
	return &Schema{compiled: s}, nil
}

// Validate runs the parsed-JSON document through the compiled schema.
// Callers that have YAML must round-trip through json.Marshal first so
// the document conforms to JSON Schema's data model. The returned
// error includes JSON Pointer locations for every offending field.
func (s *Schema) Validate(doc any) error {
	if s == nil || s.compiled == nil {
		return fmt.Errorf("schema not compiled")
	}
	return s.compiled.Validate(doc)
}
