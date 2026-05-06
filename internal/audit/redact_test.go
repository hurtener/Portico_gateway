package audit

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// deepCopyMap clones a map[string]any/[]any/string tree so a test can keep
// a pristine reference of the input and assert Redact didn't mutate it.
func deepCopyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return deepCopyMap(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = deepCopyValue(e)
		}
		return out
	default:
		return v
	}
}

func TestRedact_Bearer(t *testing.T) {
	r := NewDefaultRedactor()
	// Use a non-sensitive key so we exercise the regex path, not the
	// structural-key shortcut.
	in := map[string]any{
		"header_value": "Bearer eyJabcdef1234567890ABCDEF1234567890ABCDEF",
	}
	got := r.Redact(in)
	v, ok := got["header_value"].(string)
	if !ok {
		t.Fatalf("header_value: want string, got %T", got["header_value"])
	}
	if !strings.Contains(v, "[REDACTED:bearer]") {
		t.Fatalf("want bearer redaction, got %q", v)
	}
	if strings.Contains(v, "eyJabcdef1234567890") {
		t.Fatalf("raw token leaked: %q", v)
	}
}

func TestRedact_BearerInProse_LowEntropyShortToken_NotMatched(t *testing.T) {
	r := NewDefaultRedactor()
	in := map[string]any{
		"msg": "please send a Bearer hello message",
	}
	got := r.Redact(in)
	v := got["msg"].(string)
	if strings.Contains(v, "[REDACTED") {
		t.Fatalf("english prose got falsely redacted: %q", v)
	}
}

func TestRedact_StructuralKeyOverridesPatternMiss(t *testing.T) {
	r := NewDefaultRedactor()
	in := map[string]any{
		"client_secret": "shhh",
	}
	got := r.Redact(in)
	if got["client_secret"] != "[REDACTED:key=client_secret]" {
		t.Fatalf("structural redaction failed: %#v", got["client_secret"])
	}
}

func TestRedact_NestedMapAndSlice(t *testing.T) {
	r := NewDefaultRedactor()
	in := map[string]any{
		"outer": []any{
			map[string]any{
				"description": "uses ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA for auth",
			},
		},
	}
	got := r.Redact(in)
	outer := got["outer"].([]any)
	first := outer[0].(map[string]any)
	desc := first["description"].(string)
	if !strings.Contains(desc, "[REDACTED:github_pat]") {
		t.Fatalf("nested redaction missing: %q", desc)
	}
	if strings.Contains(desc, "ghp_AAAA") {
		t.Fatalf("raw PAT leaked: %q", desc)
	}
}

func TestRedact_DoesNotMutateInput(t *testing.T) {
	r := NewDefaultRedactor()
	in := map[string]any{
		"auth":          "Bearer eyJabcdef1234567890ABCDEF1234567890ABCDEF",
		"client_secret": "shhh",
		"nested": map[string]any{
			"items": []any{
				"ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
				"plain",
			},
			"token": "anything",
		},
	}
	snapshot := deepCopyMap(in)
	_ = r.Redact(in)
	if !reflect.DeepEqual(in, snapshot) {
		t.Fatalf("input was mutated:\nbefore: %#v\nafter:  %#v", snapshot, in)
	}
}

func TestRedact_Nil(t *testing.T) {
	r := NewDefaultRedactor()
	if got := r.Redact(nil); got != nil {
		t.Fatalf("Redact(nil) = %#v, want nil", got)
	}
}

func TestRedact_GitHubPAT(t *testing.T) {
	r := NewDefaultRedactor()
	cases := []struct {
		name        string
		in          string
		wantRedact  bool
		wantSubstr  string
		wantNoLeak  string
		description string
	}{
		{
			name:       "long_pat",
			in:         "token=ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			wantRedact: true,
			wantSubstr: "[REDACTED:github_pat]",
			wantNoLeak: "ghp_AAAA",
		},
		{
			name:       "too_short",
			in:         "token=ghp_short",
			wantRedact: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := r.Redact(map[string]any{"v": tc.in})
			v := out["v"].(string)
			if tc.wantRedact {
				if !strings.Contains(v, tc.wantSubstr) {
					t.Fatalf("want %q in %q", tc.wantSubstr, v)
				}
				if strings.Contains(v, tc.wantNoLeak) {
					t.Fatalf("raw token leaked: %q", v)
				}
			} else if v != tc.in {
				t.Fatalf("unexpected redaction of %q -> %q", tc.in, v)
			}
		})
	}
}

func TestRedact_AWSAccessKey(t *testing.T) {
	r := NewDefaultRedactor()
	cases := []struct {
		in         string
		wantRedact bool
	}{
		{"AKIAIOSFODNN7EXAMPLE", true},
		{"AKIA1234", false},
	}
	for _, tc := range cases {
		out := r.Redact(map[string]any{"v": tc.in})
		v := out["v"].(string)
		if tc.wantRedact {
			if !strings.Contains(v, "[REDACTED:aws_access_key]") {
				t.Fatalf("input %q not redacted: %q", tc.in, v)
			}
		} else if v != tc.in {
			t.Fatalf("input %q falsely redacted: %q", tc.in, v)
		}
	}
}

func TestRedact_SlackToken(t *testing.T) {
	r := NewDefaultRedactor()
	in := "xoxb-1234567890-abcdefghij"
	out := r.Redact(map[string]any{"v": in})
	v := out["v"].(string)
	if !strings.Contains(v, "[REDACTED:slack_token]") {
		t.Fatalf("slack token not redacted: %q", v)
	}
	if strings.Contains(v, "abcdefghij") {
		t.Fatalf("slack body leaked: %q", v)
	}
}

func TestRedact_JWT(t *testing.T) {
	r := NewDefaultRedactor()
	// Three base64url-ish segments separated by dots.
	in := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyIn0.signature_here_zzz"
	out := r.Redact(map[string]any{"v": in})
	v := out["v"].(string)
	if !strings.Contains(v, "[REDACTED:jwt]") {
		t.Fatalf("jwt not redacted: %q", v)
	}
	if strings.Contains(v, "signature_here_zzz") {
		t.Fatalf("jwt signature leaked: %q", v)
	}
}

func TestRedact_PrivateKeyBlock(t *testing.T) {
	r := NewDefaultRedactor()
	pem := "-----BEGIN RSA PRIVATE KEY-----\n" +
		"MIIBOgIBAAJBAKj34GkxFhD90vcNLYLInFEX6Ppy1tPf9Cnzj4p4WGeKLs1Pt8Qu\n" +
		"KUpRKfFLfRYC9AIKjbJTWit+CqvjWYzvQwECAwEAAQJAIJLixBy2qpFoS4DSmoEm\n" +
		"-----END RSA PRIVATE KEY-----"
	in := "leading text\n" + pem + "\ntrailing text"
	out := r.Redact(map[string]any{"v": in})
	v := out["v"].(string)
	if !strings.Contains(v, "[REDACTED:private_key]") {
		t.Fatalf("private key not redacted: %q", v)
	}
	if strings.Contains(v, "MIIBOgIBAAJB") {
		t.Fatalf("key body leaked: %q", v)
	}
	if !strings.Contains(v, "leading text") || !strings.Contains(v, "trailing text") {
		t.Fatalf("surrounding text dropped: %q", v)
	}
}

func TestRedact_NoFalsePositiveOnEnglishProse(t *testing.T) {
	r := NewDefaultRedactor()
	corpus := []string{
		"Please send a message about token authentication.",
		"The secret of good code is small functions.",
		"Type your password into the prompt; we never log it.",
		"Authorization is required, but Basic auth is fine here.",
		"This API key flow uses Bearer tokens at the wire level.",
	}
	for _, s := range corpus {
		out := r.Redact(map[string]any{"msg": s})
		got := out["msg"].(string)
		if got != s {
			t.Errorf("english prose redacted: %q -> %q", s, got)
		}
	}
}

func TestRedact_KnownTokenCorpus(t *testing.T) {
	r := NewDefaultRedactor()
	cases := []struct {
		name        string
		in          string
		wantSubstr  string
		wantNoLeak  string
		description string
	}{
		{
			name:       "github_classic_pat",
			in:         "ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			wantSubstr: "[REDACTED:github_pat]",
			wantNoLeak: "ghp_AAAA",
		},
		{
			name:       "github_oauth",
			in:         "gho_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
			wantSubstr: "[REDACTED:github_pat]",
			wantNoLeak: "gho_BBBB",
		},
		{
			name:       "github_user_to_server",
			in:         "ghu_CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC",
			wantSubstr: "[REDACTED:github_pat]",
			wantNoLeak: "ghu_CCCC",
		},
		{
			name:       "github_server_to_server",
			in:         "ghs_DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD",
			wantSubstr: "[REDACTED:github_pat]",
			wantNoLeak: "ghs_DDDD",
		},
		{
			name:       "github_refresh",
			in:         "ghr_EEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE",
			wantSubstr: "[REDACTED:github_pat]",
			wantNoLeak: "ghr_EEEE",
		},
		{
			name:       "aws_access_key_example",
			in:         "AKIAIOSFODNN7EXAMPLE",
			wantSubstr: "[REDACTED:aws_access_key]",
			wantNoLeak: "AKIAIOSFODNN7",
		},
		{
			name:       "slack_bot",
			in:         "xoxb-1234567890-abcdefghij",
			wantSubstr: "[REDACTED:slack_token]",
			wantNoLeak: "abcdefghij",
		},
		{
			name:       "slack_user",
			in:         "xoxp-1234567890-0987654321-abcdefghij",
			wantSubstr: "[REDACTED:slack_token]",
			wantNoLeak: "0987654321",
		},
		{
			name:       "slack_app_refresh",
			in:         "xoxr-1111111111-2222222222-zzzzzzzzzz",
			wantSubstr: "[REDACTED:slack_token]",
			wantNoLeak: "zzzzzzzzzz",
		},
		{
			name:       "bearer_jwt",
			in:         "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyIn0.aXfKZ7yz",
			wantSubstr: "[REDACTED:bearer]",
			wantNoLeak: "aXfKZ7yz",
		},
		{
			name:       "basic_auth_b64",
			in:         "Authorization: Basic dXNlcjpwYXNzd29yZHN0dWZmZmY=",
			wantSubstr: "[REDACTED:basic_auth]",
			wantNoLeak: "dXNlcjpwYXNz",
		},
		{
			name:       "jwt_standalone",
			in:         "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			wantSubstr: "[REDACTED:jwt]",
			wantNoLeak: "SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := r.Redact(map[string]any{"v": tc.in})
			v := out["v"].(string)
			if !strings.Contains(v, tc.wantSubstr) {
				t.Fatalf("want %q in %q", tc.wantSubstr, v)
			}
			if tc.wantNoLeak != "" && strings.Contains(v, tc.wantNoLeak) {
				t.Fatalf("raw secret leaked: %q", v)
			}
		})
	}
}

func TestRedact_StructuralKey_AllAliases(t *testing.T) {
	r := NewDefaultRedactor()
	for _, k := range []string{
		"token", "secret", "password", "api_key", "apikey",
		"authorization", "auth", "access_token", "client_secret",
		// case-insensitivity
		"Token", "PASSWORD", "Client_Secret",
	} {
		out := r.Redact(map[string]any{k: "literally anything"})
		got, ok := out[k].(string)
		if !ok {
			t.Fatalf("key %q: value not string: %T", k, out[k])
		}
		if !strings.HasPrefix(got, "[REDACTED:key=") {
			t.Errorf("key %q: structural redaction missing, got %q", k, got)
		}
	}
}

func TestRedact_PassThroughForUnknownTypes(t *testing.T) {
	r := NewDefaultRedactor()
	in := map[string]any{
		"count":   42,
		"enabled": true,
		"ratio":   1.5,
	}
	out := r.Redact(in)
	if out["count"] != 42 || out["enabled"] != true || out["ratio"] != 1.5 {
		t.Fatalf("non-string values mutated: %#v", out)
	}
}

func TestNewRedactor_CustomPattern(t *testing.T) {
	custom := UserPattern{
		Label: "test_marker",
		Match: regexp.MustCompile(`SECRET-[0-9]+`),
	}
	r := NewRedactor(custom)
	out := r.Redact(map[string]any{"v": "see SECRET-12345 here"})
	v := out["v"].(string)
	if !strings.Contains(v, "[REDACTED:test_marker]") {
		t.Fatalf("custom pattern did not fire: %q", v)
	}
	// The default patterns should NOT fire on this redactor — only the
	// caller-supplied set is in play.
	out2 := r.Redact(map[string]any{"v": "ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
	if strings.Contains(out2["v"].(string), "[REDACTED") {
		t.Fatalf("custom redactor unexpectedly applied default rules")
	}
}

func TestNewRedactor_StructuralKeysStillFire(t *testing.T) {
	// Even with a narrow caller-supplied pattern set, structural redaction
	// should still apply — it's the safety net for shapes the regexes miss.
	r := NewRedactor()
	out := r.Redact(map[string]any{"password": "hunter2"})
	if out["password"] != "[REDACTED:key=password]" {
		t.Fatalf("structural key not redacted under custom NewRedactor: %#v", out["password"])
	}
}

func TestRedact_NilReceiver(t *testing.T) {
	// A nil *Redactor still returns a copy without mutating input — used
	// defensively by callers that haven't wired a redactor yet.
	var r *Redactor
	in := map[string]any{"k": "v"}
	out := r.Redact(in)
	if out["k"] != "v" {
		t.Fatalf("nil receiver dropped data: %#v", out)
	}
	// Mutate the output, ensure input is unaffected.
	out["k"] = "changed"
	if in["k"] != "v" {
		t.Fatalf("nil receiver returned shared map: in=%#v", in)
	}
}
