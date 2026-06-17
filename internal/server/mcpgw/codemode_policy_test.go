package mcpgw

import (
	"context"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/mcp/codemode"
	"github.com/hurtener/Portico_gateway/internal/mcp/codemode/catalog"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// execCodeWithPolicy runs a pure-compute snippet under a given Code Mode policy
// and returns the protocol error (nil on success).
func execCodeWithPolicy(t *testing.T, pol codemode.Policy, sess *Session) *protocol.Error {
	t.Helper()
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID)
	d.SetCodeModePolicy(pol)
	_, perr := execCode(t, d, sess, "result = 1 + 1")
	return perr
}

func TestCodeModePolicy_Disabled_Denies(t *testing.T) {
	perr := execCodeWithPolicy(t, codemode.Policy{Disabled: true}, codeModeSession("p1"))
	assertGuard(t, perr, codemode.ReasonDisabled)
}

func TestCodeModePolicy_MaxExecutionBytes_Denies(t *testing.T) {
	// "result = 1 + 1" is 14 bytes; a 5-byte cap denies it.
	perr := execCodeWithPolicy(t, codemode.Policy{MaxExecutionBytes: 5}, codeModeSession("p2"))
	assertGuard(t, perr, codemode.ReasonExecutionTooLarge)
}

func TestCodeModePolicy_BindingLevelDenied(t *testing.T) {
	sess := codeModeSession("p3") // server-level binding
	perr := execCodeWithPolicy(t, codemode.Policy{AllowedBindingLevels: []string{string(catalog.BindingTool)}}, sess)
	assertGuard(t, perr, codemode.ReasonBindingLevelDenied)
}

func TestCodeModePolicy_PermissiveAllows(t *testing.T) {
	// The zero policy must not block a benign pure-compute snippet.
	if perr := execCodeWithPolicy(t, codemode.Policy{}, codeModeSession("p4")); perr != nil {
		t.Fatalf("permissive policy blocked a benign snippet: %v", perr)
	}
}

func TestCodeModePolicy_RequireApproval_NoFlow_FailsClosed(t *testing.T) {
	// require_approval with no approval flow configured must fail closed, not run.
	sess := codeModeSession("p5")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID) // NewDispatcher → no policy pipeline
	d.SetCodeModePolicy(codemode.Policy{RequireApprovalOnExecute: true})
	_, perr := execCode(t, d, sess, "result = 1")
	assertGuard(t, perr, reasonApprovalUnavailable)
}

func TestCodeModePolicy_Resume_NotGatedBySize(t *testing.T) {
	// A resume must not be re-gated on size — the original execution passed the
	// gate. We assert via the policy evaluator the handler uses: a resume with an
	// over-limit snippet is allowed.
	pol := codemode.Policy{MaxExecutionBytes: 1}
	_ = context.Background()
	d := pol.Evaluate(codemode.EvalInput{Enabled: true, CodeBytes: 9999, IsResume: true})
	if d.Deny {
		t.Fatal("resume must skip the size gate")
	}
}
