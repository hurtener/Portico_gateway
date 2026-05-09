// canonical.go produces deterministic byte-stable JSON for bundle
// persistence. Bundle exports are content-addressed: the manifest
// checksum is taken over the canonical bytes of the JSONL streams,
// which means map iteration order MUST be stable across platforms.
//
// We use a recursive sort-then-marshal approach. The two-pass cost is
// negligible at bundle size (KB–MB) and avoids any third-party dep.
//
// Phase 11.

package sessionbundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// canonicalMarshal returns a JSON encoding of v with all object keys
// sorted lexicographically and no trailing newline. Slices, primitives,
// and nested maps are handled recursively.
//
// Use canonicalMarshalLine when you want the trailing '\n' for JSONL.
func canonicalMarshal(v any) ([]byte, error) {
	normalised, err := normalise(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalised)
}

// canonicalMarshalLine is canonicalMarshal + '\n'. Used by the JSONL
// streams (one record per line).
func canonicalMarshalLine(v any) ([]byte, error) {
	b, err := canonicalMarshal(v)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(b)+1)
	out = append(out, b...)
	out = append(out, '\n')
	return out, nil
}

// normalise recursively converts v into a tree where every map[string]any
// has been replaced with a sorted-key json.RawMessage so the encoder
// produces deterministic output. Other types (slices, primitives,
// structs) round-trip via the standard encoder.
func normalise(v any) (any, error) {
	// Round-trip through json so that struct -> map conversion is
	// uniform (this lets us treat Bundle and ad-hoc maps the same way).
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	return sortMaps(generic), nil
}

// sortMaps walks a generic JSON tree and replaces every object node
// with a sorted representation. We re-marshal in the writer; the
// "sorted representation" is a json.RawMessage built from sorted keys.
func sortMaps(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			buf.Write(kb)
			buf.WriteByte(':')
			child := sortMaps(t[k])
			cb, _ := json.Marshal(child)
			buf.Write(cb)
		}
		buf.WriteByte('}')
		return json.RawMessage(buf.Bytes())
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = sortMaps(x)
		}
		return out
	default:
		return t
	}
}

// rawJSON is a tiny helper for callers that already hold pre-marshalled
// canonical bytes and want to embed them in another canonical doc.
func rawJSON(b []byte) (json.RawMessage, error) {
	if !json.Valid(b) {
		return nil, fmt.Errorf("sessionbundle: invalid embedded json")
	}
	return json.RawMessage(b), nil
}
