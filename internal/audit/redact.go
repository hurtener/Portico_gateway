package audit

import (
	"regexp"
	"strings"
)

// Redactor scrubs known credential shapes from event payloads before
// persistence. The default redactor catches bearer tokens, basic auth,
// AWS access keys, GitHub PATs, Slack tokens, generic JWTs, and PEM
// private-key blocks; operators can replace it with a custom shape via
// Store.WithRedactor.
//
// The redactor walks any map[string]any / []any tree and returns a new
// tree with offending substrings replaced by the literal "[REDACTED:label]".
// In addition, when a map key is sensitive by name (case-insensitive match
// against "token", "secret", "password", "api_key", "apikey",
// "authorization", "auth", "access_token", "client_secret"), the entire
// value is replaced with "[REDACTED:key=<keyname>]" — this catches
// shapes the regex set misses (e.g. base64-encoded passwords).
//
// Keys are preserved (so operators can tell *what* was redacted), only
// values are touched.
type Redactor struct {
	// patterns are the substring-replacement rules. The slice is iterated
	// in order; populated by NewDefaultRedactor.
	patterns []redactPattern
	// sensitiveKeys is the set of map-key names (lower-cased) whose values
	// are replaced wholesale regardless of pattern match.
	sensitiveKeys map[string]struct{}
}

// redactPattern pairs a compiled regex with the label embedded in its
// replacement.
type redactPattern struct {
	// label is included in the replacement (e.g. "[REDACTED:bearer]") so
	// operators can tell *which* rule fired without leaking the value.
	label string
	// match is the compiled regex; redactor replaces every match in any
	// string value visited.
	match *regexp.Regexp
	// repl is the replacement string fed to ReplaceAllString. Stored so we
	// can support patterns like Bearer tokens that need to keep the scheme
	// prefix in the output.
	repl string
}

// UserPattern is the public shape callers pass into NewRedactor when they
// want a Redactor with custom rules (typically tests or operators with an
// extra credential family). The replacement is always
// "[REDACTED:<Label>]" — callers don't get to control the format, so audit
// reads stay parseable.
type UserPattern struct {
	Label string
	Match *regexp.Regexp
}

// defaultSensitiveKeys is the canonical structural-redaction set. Lower-case
// because lookup compares against ToLower(key).
var defaultSensitiveKeys = map[string]struct{}{
	"token":         {},
	"secret":        {},
	"password":      {},
	"api_key":       {},
	"apikey":        {},
	"authorization": {},
	"auth":          {},
	"access_token":  {},
	"client_secret": {},
}

// Default pattern set. Compiled once at init; the Redactor itself is
// stateless past construction so the slice is shared across all
// NewDefaultRedactor() callers.
//
// Each entry's regex is intentionally conservative: it requires enough
// length / alphabet to make English-prose false positives unlikely. The
// 20-char floor on Bearer (and 16-char on Basic) is what keeps a sentence
// like "send a Bearer hello message" or "Basic auth flow" out of the
// redaction set, while a real "Bearer eyJabcdef1234567890..." still trips.
var defaultPatterns = []redactPattern{
	{
		label: "bearer",
		match: regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._\-+/=]{20,}`),
		repl:  "Bearer [REDACTED:bearer]",
	},
	{
		label: "basic_auth",
		match: regexp.MustCompile(`(?i)Basic\s+[A-Za-z0-9+/=]{16,}`),
		repl:  "Basic [REDACTED:basic_auth]",
	},
	{
		label: "github_pat",
		match: regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`),
		repl:  "[REDACTED:github_pat]",
	},
	{
		label: "aws_access_key",
		match: regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		repl:  "[REDACTED:aws_access_key]",
	},
	{
		label: "slack_token",
		match: regexp.MustCompile(`xox[abprs]-[A-Za-z0-9-]{10,}`),
		repl:  "[REDACTED:slack_token]",
	},
	{
		label: "jwt_generic",
		match: regexp.MustCompile(`eyJ[A-Za-z0-9_\-=]{10,}\.[A-Za-z0-9_\-=]+\.[A-Za-z0-9_\-=]+`),
		repl:  "[REDACTED:jwt]",
	},
	{
		label: "private_key_block",
		match: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]+?-----END [A-Z ]*PRIVATE KEY-----`),
		repl:  "[REDACTED:private_key]",
	},
}

// NewDefaultRedactor returns a Redactor with the built-in pattern set and
// the canonical sensitive-key list. Patterns are precompiled at package
// init so construction is O(1).
func NewDefaultRedactor() *Redactor {
	return &Redactor{
		patterns:      defaultPatterns,
		sensitiveKeys: defaultSensitiveKeys,
	}
}

// NewRedactor constructs a Redactor from the supplied user patterns plus
// the canonical structural sensitive-key list. Useful for tests that want
// a narrow rule set, or operators bolting on an extra credential family.
//
// Each UserPattern's replacement is always "[REDACTED:<Label>]" — callers
// don't get to pick the format so the on-disk shape stays parseable.
func NewRedactor(patterns ...UserPattern) *Redactor {
	cp := make([]redactPattern, 0, len(patterns))
	for _, p := range patterns {
		if p.Match == nil {
			continue
		}
		label := p.Label
		if label == "" {
			label = "custom"
		}
		cp = append(cp, redactPattern{
			label: label,
			match: p.Match,
			repl:  "[REDACTED:" + label + "]",
		})
	}
	return &Redactor{
		patterns:      cp,
		sensitiveKeys: defaultSensitiveKeys,
	}
}

// Redact returns a new payload with every known-credential shape replaced
// by [REDACTED:label]. nil-safe: returns nil for nil input. The input map
// is not mutated; nested maps and slices are likewise copied on the way
// down.
//
// Algorithm: walk the tree. For each string value, run every pattern's
// ReplaceAllString. For each map entry, also check whether the key is a
// sensitive name — if so, the value is replaced wholesale with
// "[REDACTED:key=<keyname>]" regardless of pattern match.
func (r *Redactor) Redact(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	if r == nil {
		// Defensive: a nil Redactor still returns a copy so callers can
		// rely on no-mutation semantics.
		out := make(map[string]any, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	return r.redactMap(in)
}

func (r *Redactor) redactMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if r.isSensitiveKey(k) {
			// Structural redaction: blow away the value regardless of shape.
			// We still recurse for non-string values so that nested
			// structures don't leak by virtue of being inside a "secret"
			// key (the wholesale label tells operators "the whole branch
			// was sensitive, here's its name").
			out[k] = "[REDACTED:key=" + k + "]"
			continue
		}
		out[k] = r.redactValue(v)
	}
	return out
}

func (r *Redactor) redactSlice(in []any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = r.redactValue(v)
	}
	return out
}

func (r *Redactor) redactValue(v any) any {
	switch x := v.(type) {
	case string:
		return r.redactString(x)
	case map[string]any:
		return r.redactMap(x)
	case []any:
		return r.redactSlice(x)
	default:
		return v
	}
}

func (r *Redactor) redactString(s string) string {
	for _, p := range r.patterns {
		if !p.match.MatchString(s) {
			// MatchString is cheaper than ReplaceAllString when there is
			// no hit, which is the common case in a hot path.
			continue
		}
		s = p.match.ReplaceAllString(s, p.repl)
	}
	return s
}

func (r *Redactor) isSensitiveKey(k string) bool {
	if len(r.sensitiveKeys) == 0 {
		return false
	}
	_, ok := r.sensitiveKeys[strings.ToLower(k)]
	return ok
}
