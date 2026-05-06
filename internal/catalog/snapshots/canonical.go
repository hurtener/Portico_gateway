package snapshots

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
)

// canonicalEncode is the deterministic JSON marshaller. Subagent owns the
// invariant suite; this implementation aims for the documented rules:
//
//   - object keys sorted ascending (string ordering, byte-wise).
//   - null values dropped from objects (so {"x": null} ≡ {}).
//   - lists keep input order; callers sort by stable key first.
//   - structs round-trip through encoding/json, then are re-encoded here
//     so the canonicalisation is uniform.
//   - json.RawMessage is parsed into `any` first, then canonicalised.
//   - basic types (string/int/float/bool) use json.Marshal — those are
//     already deterministic.
//
// Returns (canonical bytes, error). nil → "null".
func canonicalEncode(v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	// Round-trip non-trivial values through encoding/json so structs and
	// custom Marshalers reduce to map/slice/scalar form.
	switch v.(type) {
	case map[string]any, []any, string, bool, json.Number, json.RawMessage:
		// Already in a canonicaliser-friendly shape.
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var any2 any
		if err := json.Unmarshal(raw, &any2); err != nil {
			return nil, err
		}
		v = any2
	}

	var buf bytes.Buffer
	if err := canonicalWrite(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// canonicalWrite is a flat type-switch over JSON's value space. Each
// branch is a single canonical encoding rule; collapsing them into
// helpers would hide the order operators rely on for diffing fingerprint
// stability across Go versions. Carries a gocyclo waiver.
//
//nolint:gocyclo
func canonicalWrite(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
		return nil
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil
	case string:
		return writeJSONString(buf, x)
	case json.Number:
		buf.WriteString(string(x))
		return nil
	case float64:
		// Integer-valued floats without fractional part get an integer
		// representation so {"x": 1.0} hashes equal to {"x": 1}.
		if x == float64(int64(x)) && x >= -1e15 && x <= 1e15 {
			buf.WriteString(strconv.FormatInt(int64(x), 10))
			return nil
		}
		buf.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
		return nil
	case int:
		buf.WriteString(strconv.Itoa(x))
		return nil
	case int64:
		buf.WriteString(strconv.FormatInt(x, 10))
		return nil
	case map[string]any:
		return canonicalWriteMap(buf, x)
	case []any:
		return canonicalWriteSlice(buf, x)
	case json.RawMessage:
		var inner any
		if len(x) == 0 {
			buf.WriteString("null")
			return nil
		}
		if err := json.Unmarshal(x, &inner); err != nil {
			return err
		}
		return canonicalWrite(buf, inner)
	default:
		// Unknown concrete type — round-trip through json to reduce.
		raw, err := json.Marshal(x)
		if err != nil {
			return err
		}
		var inner any
		if err := json.Unmarshal(raw, &inner); err != nil {
			return err
		}
		return canonicalWrite(buf, inner)
	}
}

func canonicalWriteMap(buf *bytes.Buffer, m map[string]any) error {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v == nil {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := writeJSONString(buf, k); err != nil {
			return err
		}
		buf.WriteByte(':')
		if err := canonicalWrite(buf, m[k]); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}

func canonicalWriteSlice(buf *bytes.Buffer, s []any) error {
	buf.WriteByte('[')
	for i, v := range s {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := canonicalWrite(buf, v); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}

// writeJSONString emits a JSON-quoted string. Uses encoding/json so escape
// rules match the rest of the surface (and the result is deterministic).
func writeJSONString(buf *bytes.Buffer, s string) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	buf.Write(b)
	return nil
}
