package api

import (
	"bytes"
	"encoding/json"
)

// jsonEncode is a small wrapper around encoding/json.Marshal that returns
// compact bytes. Lifts repeated buffer plumbing out of the audit helpers.
func jsonEncode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	// json.Encoder appends a newline. Strip it for stable hashing.
	out := buf.Bytes()
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	return out, nil
}
