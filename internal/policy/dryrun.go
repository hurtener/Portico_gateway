package policy

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"
)

// ToolCallShape is the synthetic call the dry-run evaluates against the
// ruleset. It mirrors the runtime fields the engine cares about, minus
// the southbound state.
type ToolCallShape struct {
	TenantID string         `json:"tenant_id"`
	Server   string         `json:"server"`
	Tool     string         `json:"tool"`
	Args     map[string]any `json:"args,omitempty"`
	Now      time.Time      `json:"now,omitempty"`
}

// RuleMatch records why a rule matched (or "lost" on priority). Reason is
// human readable so the Console can render it directly.
type RuleMatch struct {
	RuleID   string `json:"rule_id"`
	Priority int    `json:"priority"`
	Reason   string `json:"reason"`
}

// DryRunResult is what the editor's right pane renders. MatchedRules is
// the rules that voted on the outcome (in priority order); LosingRules is
// rules that matched but were overridden. FinalAction collapses every
// matched rule's Actions into one effective verdict.
type DryRunResult struct {
	MatchedRules []RuleMatch `json:"matched_rules"`
	LosingRules  []RuleMatch `json:"losing_rules,omitempty"`
	FinalAction  Actions     `json:"final_action"`
	FinalRisk    string      `json:"final_risk"`
}

// DryRun evaluates rules against call. Lower priority wins (mirrors the
// engine convention). Deny short-circuits Allow at the highest priority
// match; otherwise the first Allow stays. RequireApproval is sticky —
// any matched rule that carries it adds approval to the final action.
func DryRun(_ context.Context, rs RuleSet, call ToolCallShape) DryRunResult {
	if call.Now.IsZero() {
		call.Now = time.Now().UTC()
	}
	res := DryRunResult{}
	// Walk rules in priority order so the lowest priority that matches
	// wins on the verdict; ties broken by rule ID for stability.
	rules := orderedRules(rs)
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if !ruleMatches(r, call) {
			continue
		}
		match := RuleMatch{RuleID: r.ID, Priority: r.Priority, Reason: matchReason(r, call)}
		// First match wins on the verdict; subsequent matches are
		// recorded as losing rules unless they only annotate
		// (RequireApproval / annotate / log_level).
		if len(res.MatchedRules) == 0 {
			res.MatchedRules = append(res.MatchedRules, match)
			res.FinalAction = r.Actions
			if r.RiskClass != "" {
				res.FinalRisk = r.RiskClass
			}
			if r.Actions.AnnotateRiskClass != "" {
				res.FinalRisk = r.Actions.AnnotateRiskClass
			}
			continue
		}
		// Sticky annotations — RequireApproval, AnnotateRiskClass, LogLevel
		// stack on top of the winning verdict.
		stuck := false
		if r.Actions.RequireApproval {
			res.FinalAction.RequireApproval = true
			stuck = true
		}
		if r.Actions.AnnotateRiskClass != "" {
			res.FinalAction.AnnotateRiskClass = r.Actions.AnnotateRiskClass
			res.FinalRisk = r.Actions.AnnotateRiskClass
			stuck = true
		}
		if r.Actions.LogLevel != "" && res.FinalAction.LogLevel == "" {
			res.FinalAction.LogLevel = r.Actions.LogLevel
			stuck = true
		}
		if stuck {
			res.MatchedRules = append(res.MatchedRules, match)
		} else {
			res.LosingRules = append(res.LosingRules, match)
		}
	}
	if res.FinalRisk == "" {
		res.FinalRisk = RiskWrite
	}
	return res
}

// orderedRules returns rules sorted by priority asc, id asc.
func orderedRules(rs RuleSet) []Rule {
	out := make([]Rule, len(rs.Rules))
	copy(out, rs.Rules)
	// Insertion sort — ruleset sizes are tiny.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := out[j-1], out[j]
			if a.Priority < b.Priority || (a.Priority == b.Priority && a.ID <= b.ID) {
				break
			}
			out[j-1], out[j] = b, a
		}
	}
	return out
}

func ruleMatches(r Rule, call ToolCallShape) bool {
	m := r.Conditions.Match
	if len(m.Tenants) > 0 && !contains(m.Tenants, call.TenantID) {
		return false
	}
	if len(m.Servers) > 0 && !contains(m.Servers, call.Server) {
		return false
	}
	if len(m.Tools) > 0 {
		full := call.Tool
		if call.Server != "" && !strings.Contains(call.Tool, ".") {
			full = call.Server + "." + call.Tool
		}
		if !globMatchAny(m.Tools, full) && !globMatchAny(m.Tools, call.Tool) {
			return false
		}
	}
	if m.TimeRange.From != "" || m.TimeRange.To != "" {
		if !inTimeRange(call.Now, m.TimeRange.From, m.TimeRange.To) {
			return false
		}
	}
	if m.ArgsExpr != "" {
		// V1: only support `key=value` exact matches comma-separated.
		if !argsExprMatches(m.ArgsExpr, call.Args) {
			return false
		}
	}
	return true
}

func matchReason(r Rule, call ToolCallShape) string {
	parts := []string{}
	m := r.Conditions.Match
	if len(m.Tools) > 0 {
		parts = append(parts, "tool="+call.Tool)
	}
	if len(m.Servers) > 0 {
		parts = append(parts, "server="+call.Server)
	}
	if len(m.Tenants) > 0 {
		parts = append(parts, "tenant="+call.TenantID)
	}
	if m.TimeRange.From != "" || m.TimeRange.To != "" {
		parts = append(parts, "time-range")
	}
	if len(parts) == 0 {
		return "default match (no conditions)"
	}
	return strings.Join(parts, ", ")
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func globMatchAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if p == "" {
			continue
		}
		if p == "*" || p == name {
			return true
		}
		if matched, err := path.Match(p, name); err == nil && matched {
			return true
		}
		if strings.HasSuffix(p, "*") && strings.HasPrefix(name, strings.TrimSuffix(p, "*")) {
			return true
		}
	}
	return false
}

func inTimeRange(now time.Time, fromStr, toStr string) bool {
	hhmm := now.UTC().Format("15:04")
	from := fromStr
	to := toStr
	if from == "" {
		from = "00:00"
	}
	if to == "" {
		to = "23:59"
	}
	// Compare strings — works because HH:MM lexicographic order == time.
	if from <= to {
		return hhmm >= from && hhmm <= to
	}
	// Wraps midnight (e.g. 22:00..06:00).
	return hhmm >= from || hhmm <= to
}

// argsExprMatches handles the V1 expression dialect: comma-separated
// "key=value" pairs. All of them must hold for the expression to match.
func argsExprMatches(expr string, args map[string]any) bool {
	for _, term := range strings.Split(expr, ",") {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		eq := strings.IndexByte(term, '=')
		if eq < 1 {
			return false
		}
		k := strings.TrimSpace(term[:eq])
		v := strings.TrimSpace(term[eq+1:])
		got, ok := args[k]
		if !ok {
			return false
		}
		// Compare via fmt-string so int/float/bool surface as strings.
		gotStr := stringify(got)
		if gotStr != v {
			return false
		}
	}
	return true
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", t)
	}
}
