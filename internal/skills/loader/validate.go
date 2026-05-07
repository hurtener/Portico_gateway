// validate.go is the canonical validation pipeline shared by the
// loader's load path, the REST validate endpoint, and the authoring
// UI's live validation panel. Every error carries a JSON Pointer +
// (when available) the YAML line/column the manifest declared it on.

package loader

import (
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
)

// Violation is one structured validation finding.
type Violation struct {
	Pointer string `json:"pointer"`
	Line    int    `json:"line,omitempty"`
	Col     int    `json:"col,omitempty"`
	Reason  string `json:"reason"`
	Kind    string `json:"kind,omitempty"` // "schema" | "semantic"
}

// ValidationResult is the canonical output. ManifestRaw is the
// canonical YAML/JSON the validator parsed (handed back so callers
// can persist or display the same body the validator inspected).
type ValidationResult struct {
	Violations []Violation `json:"violations"`
	Warnings   []string    `json:"warnings,omitempty"`
	Manifest   *manifest.Manifest
}

// HasErrors reports whether any blocking violation was recorded.
func (r ValidationResult) HasErrors() bool { return len(r.Violations) > 0 }

// ValidateManifestBytes runs the schema + (optional) semantic checks
// on a manifest body. The body may be YAML or JSON; line/col are
// extracted from the YAML node tree when available.
func ValidateManifestBytes(body []byte, schema *manifest.Schema) ValidationResult {
	res := ValidationResult{}

	m, doc, err := manifest.Parse(body)
	if err != nil {
		res.Violations = append(res.Violations, Violation{
			Pointer: "",
			Reason:  err.Error(),
			Kind:    "schema",
		})
		return res
	}
	res.Manifest = m

	if schema != nil {
		if err := schema.Validate(doc); err != nil {
			res.Violations = append(res.Violations, schemaErrorToViolations(err, body)...)
		}
	}
	return res
}

// schemaErrorToViolations flattens a santhosh-tekuri/jsonschema error
// tree into Violation rows. Each leaf "Cause" becomes one violation;
// the path is the JSON Pointer derived from InstanceLocation.
func schemaErrorToViolations(err error, body []byte) []Violation {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return []Violation{{Pointer: "", Reason: err.Error(), Kind: "schema"}}
	}
	out := make([]Violation, 0)
	walkValidation(ve, body, &out)
	if len(out) == 0 {
		out = append(out, Violation{Pointer: "", Reason: ve.Error(), Kind: "schema"})
	}
	return out
}

func walkValidation(v *jsonschema.ValidationError, body []byte, out *[]Violation) {
	if len(v.Causes) == 0 {
		ptr := pointerFromLocation(v.InstanceLocation)
		line, col := lineColForPointer(body, ptr)
		*out = append(*out, Violation{
			Pointer: ptr,
			Line:    line,
			Col:     col,
			Reason:  reasonFromKind(v),
			Kind:    "schema",
		})
		return
	}
	for _, c := range v.Causes {
		walkValidation(c, body, out)
	}
}

func pointerFromLocation(loc []string) string {
	if len(loc) == 0 {
		return ""
	}
	var b strings.Builder
	for _, seg := range loc {
		b.WriteByte('/')
		b.WriteString(escapeJSONPointer(seg))
	}
	return b.String()
}

func escapeJSONPointer(seg string) string {
	seg = strings.ReplaceAll(seg, "~", "~0")
	seg = strings.ReplaceAll(seg, "/", "~1")
	return seg
}

func reasonFromKind(v *jsonschema.ValidationError) string {
	if v == nil {
		return ""
	}
	// LocalizedString(nil) can panic for some kinds; fall back to the
	// stringified error message which always renders.
	defer func() { _ = recover() }()
	if v.ErrorKind != nil {
		if path := v.ErrorKind.KeywordPath(); len(path) > 0 {
			return fmt.Sprintf("%s: %s", strings.Join(path, "/"), v.Error())
		}
	}
	return v.Error()
}

// lineColForPointer walks the YAML node tree to find the byte location
// of a JSON-Pointer path. Returns (0, 0) when the lookup fails — the
// validator falls back to the schema URL/path; the UI degrades to a
// pointer breadcrumb without inline highlight.
func lineColForPointer(body []byte, pointer string) (int, int) {
	if len(body) == 0 || pointer == "" {
		return 0, 0
	}
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return 0, 0
	}
	cur := &root
	if cur.Kind == yaml.DocumentNode && len(cur.Content) > 0 {
		cur = cur.Content[0]
	}
	if pointer == "" || pointer == "/" {
		return cur.Line, cur.Column
	}
	segs := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	for _, seg := range segs {
		seg = strings.ReplaceAll(seg, "~1", "/")
		seg = strings.ReplaceAll(seg, "~0", "~")
		next := descend(cur, seg)
		if next == nil {
			return cur.Line, cur.Column
		}
		cur = next
	}
	return cur.Line, cur.Column
}

func descend(n *yaml.Node, key string) *yaml.Node {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				return n.Content[i+1]
			}
		}
	case yaml.SequenceNode:
		// numeric index
		idx := 0
		for _, r := range key {
			if r < '0' || r > '9' {
				return nil
			}
			idx = idx*10 + int(r-'0')
		}
		if idx >= 0 && idx < len(n.Content) {
			return n.Content[idx]
		}
	case yaml.DocumentNode, yaml.ScalarNode, yaml.AliasNode:
		// Not addressable by key — fall through to the nil return.
	}
	return nil
}
