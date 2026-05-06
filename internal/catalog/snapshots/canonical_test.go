package snapshots

import (
	"bytes"
	"encoding/json"
	"testing"
)

// canonicalEncode is the package-private deterministic JSON marshaller. The
// tests live inside the same package so they can drive it directly without
// inflating the public surface.

func TestCanonical_KeysSorted(t *testing.T) {
	t.Parallel()
	a := map[string]any{"b": 1, "a": 2}
	b := map[string]any{"a": 2, "b": 1}
	const want = `{"a":2,"b":1}`
	for _, in := range []map[string]any{a, b} {
		got, err := canonicalEncode(in)
		if err != nil {
			t.Fatalf("canonicalEncode: %v", err)
		}
		if string(got) != want {
			t.Errorf("canonicalEncode(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonical_NullValuesDropped(t *testing.T) {
	t.Parallel()
	in := map[string]any{"x": nil, "y": 1}
	got, err := canonicalEncode(in)
	if err != nil {
		t.Fatalf("canonicalEncode: %v", err)
	}
	if string(got) != `{"y":1}` {
		t.Errorf("canonicalEncode = %q, want {\"y\":1}", got)
	}
}

func TestCanonical_NestedDeterministic(t *testing.T) {
	t.Parallel()
	// Build a multi-level structure with several map entries; the Go map
	// iteration order is randomised per run, and round-tripping through
	// json.Marshal/Unmarshal exercises that randomness on every iteration.
	build := func() any {
		return map[string]any{
			"alpha":   1,
			"bravo":   "two",
			"charlie": []any{3, 4, 5},
			"delta": map[string]any{
				"e": map[string]any{"f": 6, "g": 7, "h": []any{8, 9}},
				"i": "j",
				"k": []any{
					map[string]any{"l": 10, "m": 11},
					map[string]any{"n": 12, "o": 13},
				},
			},
			"epsilon": map[string]any{"p": nil, "q": 14},
		}
	}

	first, err := canonicalEncode(build())
	if err != nil {
		t.Fatalf("canonicalEncode: %v", err)
	}
	for i := 0; i < 200; i++ {
		// Round-trip through encoding/json so we re-shuffle map ordering
		// inside the unmarshalled structure on every iteration.
		raw, err := json.Marshal(build())
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var any2 any
		if err := json.Unmarshal(raw, &any2); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		got, err := canonicalEncode(any2)
		if err != nil {
			t.Fatalf("canonicalEncode: %v", err)
		}
		if !bytes.Equal(first, got) {
			t.Fatalf("iteration %d: canonical output diverged\n first=%s\n  got=%s", i, first, got)
		}
	}
}

func TestCanonical_RoundTripsStruct(t *testing.T) {
	t.Parallel()
	type inner struct {
		B int `json:"b"`
		A int `json:"a"`
	}
	type outer struct {
		Y inner  `json:"y"`
		X string `json:"x"`
	}
	got, err := canonicalEncode(outer{Y: inner{B: 1, A: 2}, X: "hi"})
	if err != nil {
		t.Fatalf("canonicalEncode: %v", err)
	}
	const want = `{"x":"hi","y":{"a":2,"b":1}}`
	if string(got) != want {
		t.Errorf("canonicalEncode = %q, want %q", got, want)
	}
}

func TestCanonical_HandlesRawMessage(t *testing.T) {
	t.Parallel()
	rm := json.RawMessage(`{"b":1,"a":2}`)
	got, err := canonicalEncode(rm)
	if err != nil {
		t.Fatalf("canonicalEncode: %v", err)
	}
	const want = `{"a":2,"b":1}`
	if string(got) != want {
		t.Errorf("canonicalEncode RawMessage = %q, want %q", got, want)
	}
}

func TestCanonical_FloatIntEquivalence(t *testing.T) {
	t.Parallel()
	asFloat, err := canonicalEncode(map[string]any{"x": 1.0})
	if err != nil {
		t.Fatalf("canonicalEncode float: %v", err)
	}
	asInt, err := canonicalEncode(map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("canonicalEncode int: %v", err)
	}
	if !bytes.Equal(asFloat, asInt) {
		t.Errorf("integer-valued float should encode like int: float=%s int=%s", asFloat, asInt)
	}
	if string(asInt) != `{"x":1}` {
		t.Errorf("expected {\"x\":1}, got %s", asInt)
	}
}

func TestCanonical_EmptyInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, "null"},
		{"empty-map", map[string]any{}, "{}"},
		{"empty-slice", []any{}, "[]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := canonicalEncode(tc.in)
			if err != nil {
				t.Fatalf("canonicalEncode: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("canonicalEncode(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCanonical_StringEscaping(t *testing.T) {
	t.Parallel()
	cases := []string{
		`hello "world"`,
		`back\\slash`,
		"line\nbreak",
		"unicode: ñ é 漢字",
		"control: \t \r",
	}
	for _, s := range cases {
		got, err := canonicalEncode(s)
		if err != nil {
			t.Fatalf("canonicalEncode(%q): %v", s, err)
		}
		want, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("json.Marshal(%q): %v", s, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("string escape mismatch for %q: got %s, want %s", s, got, want)
		}
	}
}
