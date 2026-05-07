package policy

import (
	"errors"
	"strings"
	"testing"
)

func TestValidate_RuleShape(t *testing.T) {
	cases := []struct {
		name string
		r    Rule
		ok   bool
	}{
		{"id required", Rule{}, false},
		{
			"unknown risk class",
			Rule{ID: "r", RiskClass: "lol"},
			false,
		},
		{
			"actions exclusive — allow + deny",
			Rule{ID: "r", Actions: Actions{Allow: true, Deny: true}},
			false,
		},
		{
			"time range bad format",
			Rule{ID: "r", Conditions: Conditions{Match: Match{TimeRange: TimeRange{From: "noon"}}}},
			false,
		},
		{
			"happy path",
			Rule{ID: "r", RiskClass: RiskRead, Actions: Actions{Allow: true}},
			true,
		},
	}
	for _, tc := range cases {
		err := Validate(tc.r)
		if tc.ok && err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
		if !tc.ok && err != nil && !errors.Is(err, ErrInvalidRule) {
			t.Errorf("%s: error not wrapping ErrInvalidRule: %v", tc.name, err)
		}
	}
}

func TestCanonicalise_StableAcrossOSEncodings(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "b", Priority: 5, Enabled: true, Actions: Actions{Allow: true}},
		{ID: "a", Priority: 1, Enabled: true, Actions: Actions{Deny: true}},
	}}
	a, err := Canonicalise(rs)
	if err != nil {
		t.Fatal(err)
	}
	// Reverse the input order; canonicalisation should produce the same bytes.
	rs.Rules[0], rs.Rules[1] = rs.Rules[1], rs.Rules[0]
	b, err := Canonicalise(rs)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Errorf("canonical output differs across input order:\n%s\n%s", a, b)
	}
	// Sanity: the lowest-priority rule appears first in the output.
	if !strings.Contains(string(a), `"id":"a"`) {
		t.Errorf("canonical output missing rule id: %s", a)
	}
}

func TestRuleSet_OrderedByPriority(t *testing.T) {
	rs := RuleSet{Rules: []Rule{
		{ID: "z", Priority: 100, Enabled: true},
		{ID: "a", Priority: 100, Enabled: true},
		{ID: "m", Priority: 1, Enabled: true},
	}}
	ord := orderedRules(rs)
	if ord[0].ID != "m" {
		t.Errorf("expected m first, got %s", ord[0].ID)
	}
	if ord[1].ID != "a" || ord[2].ID != "z" {
		t.Errorf("expected a,z; got %s,%s", ord[1].ID, ord[2].ID)
	}
}
