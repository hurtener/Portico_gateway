package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
)

// Phase 9 introduces an editable, SQL-backed policy ruleset. The shapes
// here are the public surface the REST handlers and Console talk to. The
// engine continues to consume the simpler tenant-scoped Policy type for
// backwards compatibility — rules synthesise that shape via Compile.

// RuleSet is an ordered list of rules. Order is by ascending Priority,
// then ascending RuleID for stable rendering.
type RuleSet struct {
	Rules []Rule `json:"rules"`
}

// Rule is one editable rule. Conditions and Actions are structured so the
// editor can present them as forms, and so the dry-run can evaluate them.
// The canonical YAML representation is produced by Canonicalise so the
// "raw YAML" toggle in the editor round-trips byte-for-byte.
type Rule struct {
	ID         string     `json:"id" yaml:"id"`
	Priority   int        `json:"priority" yaml:"priority"`
	Enabled    bool       `json:"enabled" yaml:"enabled"`
	RiskClass  string     `json:"risk_class" yaml:"risk_class"`
	Conditions Conditions `json:"conditions" yaml:"conditions"`
	Actions    Actions    `json:"actions" yaml:"actions"`
	Notes      string     `json:"notes,omitempty" yaml:"notes,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
	UpdatedBy  string     `json:"updated_by,omitempty" yaml:"updated_by,omitempty"`
}

// Conditions describes what a rule matches. Empty match = match everything.
type Conditions struct {
	Match Match `json:"match" yaml:"match"`
}

// Match holds the structured matchers the editor can render as form
// fields. ArgsExpr remains a freeform string for V1 (the engine ignores it
// today; the dry-run honours it via the simple equals heuristic).
type Match struct {
	Tools     []string  `json:"tools,omitempty" yaml:"tools,omitempty"`
	Servers   []string  `json:"servers,omitempty" yaml:"servers,omitempty"`
	Tenants   []string  `json:"tenants,omitempty" yaml:"tenants,omitempty"`
	ArgsExpr  string    `json:"args_expr,omitempty" yaml:"args_expr,omitempty"`
	TimeRange TimeRange `json:"time_range,omitempty" yaml:"time_range,omitempty"`
}

// TimeRange is an HH:MM..HH:MM window in UTC. Empty fields disable the
// check on that side.
type TimeRange struct {
	From string `json:"from,omitempty" yaml:"from,omitempty"`
	To   string `json:"to,omitempty" yaml:"to,omitempty"`
}

// Actions captures the rule's outcome. Allow / Deny / RequireApproval are
// mutually exclusive at the top level (the validator enforces it). The
// engine treats Allow=false && Deny=false as a no-op rule (used for
// annotation only).
type Actions struct {
	Allow             bool   `json:"allow,omitempty" yaml:"allow,omitempty"`
	Deny              bool   `json:"deny,omitempty" yaml:"deny,omitempty"`
	RequireApproval   bool   `json:"require_approval,omitempty" yaml:"require_approval,omitempty"`
	LogLevel          string `json:"log_level,omitempty" yaml:"log_level,omitempty"`
	AnnotateRiskClass string `json:"annotate,omitempty" yaml:"annotate,omitempty"`
}

// ErrInvalidRule is the sentinel returned by Validate. Wrapped errors are
// formatted with the offending field so the Console can highlight it.
var ErrInvalidRule = errors.New("policy: invalid rule")

// Validate enforces the editor's invariants:
//   - ID non-empty.
//   - RiskClass is in the canonical set (risk classes from riskclass.go).
//   - At most one of Allow / Deny / RequireApproval is true.
//   - TimeRange entries, when present, match HH:MM.
func Validate(r Rule) error {
	if r.ID == "" {
		return fmt.Errorf("%w: id required", ErrInvalidRule)
	}
	if r.RiskClass != "" {
		if _, ok := validRiskClasses[r.RiskClass]; !ok {
			return fmt.Errorf("%w: risk_class %q unknown", ErrInvalidRule, r.RiskClass)
		}
	}
	count := 0
	if r.Actions.Allow {
		count++
	}
	if r.Actions.Deny {
		count++
	}
	if r.Actions.RequireApproval {
		count++
	}
	if count > 1 {
		return fmt.Errorf("%w: actions allow/deny/require_approval are mutually exclusive", ErrInvalidRule)
	}
	if r.Conditions.Match.TimeRange.From != "" {
		if _, err := time.Parse("15:04", r.Conditions.Match.TimeRange.From); err != nil {
			return fmt.Errorf("%w: time_range.from must be HH:MM", ErrInvalidRule)
		}
	}
	if r.Conditions.Match.TimeRange.To != "" {
		if _, err := time.Parse("15:04", r.Conditions.Match.TimeRange.To); err != nil {
			return fmt.Errorf("%w: time_range.to must be HH:MM", ErrInvalidRule)
		}
	}
	return nil
}

// Canonicalise produces a deterministic JSON representation of the
// ruleset suitable for hashing or storing on disk. We use canonical JSON
// (the shared snapshots canonicaliser) rather than YAML for V1 — the
// Console renders YAML for humans via the standard `yaml.Marshal` path,
// but the on-disk + on-wire canonical form is JSON to match every other
// hash-sensitive surface.
func Canonicalise(rs RuleSet) ([]byte, error) {
	// Sort rules by (priority, id) so the output is stable irrespective
	// of insertion order.
	cp := make([]Rule, len(rs.Rules))
	copy(cp, rs.Rules)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].Priority != cp[j].Priority {
			return cp[i].Priority < cp[j].Priority
		}
		return cp[i].ID < cp[j].ID
	})
	// Round-trip through json.RawMessage so the canonical encoder sees
	// our struct as a plain map[string]any.
	raw, err := json.Marshal(struct {
		Rules []Rule `json:"rules"`
	}{Rules: cp})
	if err != nil {
		return nil, err
	}
	var any map[string]interface{}
	if err := json.Unmarshal(raw, &any); err != nil {
		return nil, err
	}
	return snapshots.CanonicalEncode(any)
}

// EncodeConditions / EncodeActions return canonical JSON for the storage
// layer. Splitting them out lets the SQL store keep the conditions and
// actions columns hashable independently.
func EncodeConditions(c Conditions) ([]byte, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	var any map[string]interface{}
	if err := json.Unmarshal(raw, &any); err != nil {
		return nil, err
	}
	return snapshots.CanonicalEncode(any)
}

// EncodeActions canonicalises the actions block.
func EncodeActions(a Actions) ([]byte, error) {
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	var any map[string]interface{}
	if err := json.Unmarshal(raw, &any); err != nil {
		return nil, err
	}
	return snapshots.CanonicalEncode(any)
}

// DecodeConditions parses a canonical-JSON conditions blob.
func DecodeConditions(b []byte) (Conditions, error) {
	var c Conditions
	if len(b) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Conditions{}, err
	}
	return c, nil
}

// DecodeActions parses a canonical-JSON actions blob.
func DecodeActions(b []byte) (Actions, error) {
	var a Actions
	if len(b) == 0 {
		return a, nil
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return Actions{}, err
	}
	return a, nil
}
