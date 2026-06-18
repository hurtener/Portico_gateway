package budgets

import (
	"errors"
	"testing"
	"time"
)

// ref is a fixed reference instant used across tests: Tuesday 2026-05-12
// 13:47:29 UTC. Using a shared constant keeps the table tests readable and
// guarantees every assertion runs against the same civil/rolling boundaries.
var ref = time.Date(2026, 5, 12, 13, 47, 29, 0, time.UTC)

// TestWindowFor_Calendar asserts the exact Start/ResetsAt/Key for every
// period under calendar alignment against the fixed reference instant.
func TestWindowFor_Calendar(t *testing.T) {
	tests := []struct {
		name     string
		period   Period
		start    time.Time
		resetsAt time.Time
		key      string
	}{
		{"1m", Period1m,
			time.Date(2026, 5, 12, 13, 47, 0, 0, time.UTC),
			time.Date(2026, 5, 12, 13, 48, 0, 0, time.UTC),
			"cal:1m:2026-05-12T13:47"},
		{"1h", Period1h,
			time.Date(2026, 5, 12, 13, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC),
			"cal:1h:2026-05-12T13"},
		{"1d", Period1d,
			time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
			"cal:1d:2026-05-12"},
		{"1w", Period1w,
			time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC),
			"cal:1w:2026-05-11"},
		{"1M", Period1M,
			time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			"cal:1M:2026-05"},
		{"1Y", Period1Y,
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
			"cal:1Y:2026"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, err := WindowFor(ref, tc.period, AlignCalendar)
			if err != nil {
				t.Fatalf("WindowFor: unexpected error: %v", err)
			}
			if !w.Start.Equal(tc.start) {
				t.Errorf("Start: got %s, want %s", w.Start, tc.start)
			}
			if !w.ResetsAt.Equal(tc.resetsAt) {
				t.Errorf("ResetsAt: got %s, want %s", w.ResetsAt, tc.resetsAt)
			}
			if w.Key != tc.key {
				t.Errorf("Key: got %q, want %q", w.Key, tc.key)
			}
		})
	}
}

// TestWindowFor_Invariants walks every period x alignment combo and asserts
// the structural invariants: Start <= ref, ResetsAt strictly after ref, and
// ResetsAt strictly after Start (every window has positive width). It also
// guards against empty keys.
func TestWindowFor_Invariants(t *testing.T) {
	periods := []Period{Period1m, Period1h, Period1d, Period1w, Period1M, Period1Y}
	aligns := []Alignment{AlignCalendar, AlignRolling}

	for _, p := range periods {
		for _, a := range aligns {
			w, err := WindowFor(ref, p, a)
			if err != nil {
				t.Errorf("period=%s align=%s: unexpected error: %v", p, a, err)
				continue
			}
			if !(w.Start.Before(ref) || w.Start.Equal(ref)) {
				t.Errorf("period=%s align=%s: Start %s not <= ref %s", p, a, w.Start, ref)
			}
			if !w.ResetsAt.After(ref) {
				t.Errorf("period=%s align=%s: ResetsAt %s not after ref %s", p, a, w.ResetsAt, ref)
			}
			if !w.ResetsAt.After(w.Start) {
				t.Errorf("period=%s align=%s: ResetsAt %s not after Start %s", p, a, w.ResetsAt, w.Start)
			}
			if w.Key == "" {
				t.Errorf("period=%s align=%s: empty Key", p, a)
			}
		}
	}
}

// TestWindowFor_Rolling_SameBucket asserts two instants in the same rolling
// bucket share a Key, and that an instant one duration later produces a
// different Key whose Start and ResetsAt are advanced by exactly the
// duration. Covers 1m and 1d.
func TestWindowFor_Rolling_SameBucket(t *testing.T) {
	cases := []struct {
		name   string
		period Period
		dur    time.Duration
	}{
		{"1m", Period1m, time.Minute},
		{"1d", Period1d, 24 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w1, err := WindowFor(ref, c.period, AlignRolling)
			if err != nil {
				t.Fatalf("w1: %v", err)
			}
			// 1s later stays inside the same bucket: ref sits at 13:47:29, so
			// for 1m there is 31s of headroom and for 1d there is ~10h.
			w2, err := WindowFor(ref.Add(time.Second), c.period, AlignRolling)
			if err != nil {
				t.Fatalf("w2: %v", err)
			}
			if w1.Key != w2.Key {
				t.Errorf("same bucket: keys differ: %q vs %q", w1.Key, w2.Key)
			}

			// Exactly one duration later → next bucket, advanced by dur.
			w3, err := WindowFor(ref.Add(c.dur), c.period, AlignRolling)
			if err != nil {
				t.Fatalf("w3: %v", err)
			}
			if w3.Key == w1.Key {
				t.Errorf("expected different key after one duration, got same %q", w1.Key)
			}
			if !w3.Start.Equal(w1.Start.Add(c.dur)) {
				t.Errorf("Start advanced by dur: got %s, want %s", w3.Start, w1.Start.Add(c.dur))
			}
			if !w3.ResetsAt.Equal(w1.ResetsAt.Add(c.dur)) {
				t.Errorf("ResetsAt advanced by dur: got %s, want %s", w3.ResetsAt, w1.ResetsAt.Add(c.dur))
			}
		})
	}
}

// TestWindowFor_TimezoneIndependence asserts that feeding the same instant
// through a non-UTC zone yields the identical Window, because WindowFor
// converts to UTC at the top.
func TestWindowFor_TimezoneIndependence(t *testing.T) {
	zone := time.FixedZone("x", 5*3600) // UTC+5
	refZoned := ref.In(zone)

	periods := []Period{Period1m, Period1h, Period1d, Period1w, Period1M, Period1Y}
	aligns := []Alignment{AlignCalendar, AlignRolling}

	for _, p := range periods {
		for _, a := range aligns {
			w1, err1 := WindowFor(ref, p, a)
			w2, err2 := WindowFor(refZoned, p, a)
			if err1 != nil || err2 != nil {
				t.Errorf("period=%s align=%s: errors: %v %v", p, a, err1, err2)
				continue
			}
			if w1.Key != w2.Key || !w1.Start.Equal(w2.Start) || !w1.ResetsAt.Equal(w2.ResetsAt) {
				t.Errorf("period=%s align=%s: timezone changed window: %+v vs %+v", p, a, w1, w2)
			}
		}
	}
}

// TestParsePeriod covers valid round-trips and invalid inputs (errors.Is).
func TestParsePeriod(t *testing.T) {
	valid := map[string]Period{
		"1m": Period1m, "1h": Period1h, "1d": Period1d,
		"1w": Period1w, "1M": Period1M, "1Y": Period1Y,
	}
	for s, want := range valid {
		got, err := ParsePeriod(s)
		if err != nil {
			t.Errorf("ParsePeriod(%q): unexpected error: %v", s, err)
			continue
		}
		if got != want {
			t.Errorf("ParsePeriod(%q): got %s, want %s", s, got, want)
		}
	}
	for _, bad := range []string{"", "2m", "month", "1mo", "1y", "60s"} {
		if _, err := ParsePeriod(bad); !errors.Is(err, ErrUnknownPeriod) {
			t.Errorf("ParsePeriod(%q): want ErrUnknownPeriod, got %v", bad, err)
		}
	}
}

// TestParseAlignment covers valid round-trips and invalid inputs (errors.Is).
func TestParseAlignment(t *testing.T) {
	for s, want := range map[string]Alignment{
		"calendar": AlignCalendar, "rolling": AlignRolling,
	} {
		got, err := ParseAlignment(s)
		if err != nil {
			t.Errorf("ParseAlignment(%q): unexpected error: %v", s, err)
			continue
		}
		if got != want {
			t.Errorf("ParseAlignment(%q): got %s, want %s", s, got, want)
		}
	}
	for _, bad := range []string{"", "Calendar", "ROLLED", "utc", "epoch"} {
		if _, err := ParseAlignment(bad); !errors.Is(err, ErrUnknownAlignment) {
			t.Errorf("ParseAlignment(%q): want ErrUnknownAlignment, got %v", bad, err)
		}
	}
}

// TestWindowFor_UnknownInputs asserts WindowFor returns the correct sentinel
// for an unknown period (under both alignments) and an unknown alignment.
func TestWindowFor_UnknownInputs(t *testing.T) {
	if _, err := WindowFor(ref, Period("bogus"), AlignCalendar); !errors.Is(err, ErrUnknownPeriod) {
		t.Errorf("unknown period calendar: want ErrUnknownPeriod, got %v", err)
	}
	if _, err := WindowFor(ref, Period("bogus"), AlignRolling); !errors.Is(err, ErrUnknownPeriod) {
		t.Errorf("unknown period rolling: want ErrUnknownPeriod, got %v", err)
	}
	if _, err := WindowFor(ref, Period1m, Alignment("bogus")); !errors.Is(err, ErrUnknownAlignment) {
		t.Errorf("unknown alignment: want ErrUnknownAlignment, got %v", err)
	}
}
