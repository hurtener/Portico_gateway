package mcpgw

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// fakeCodeModeStore is a minimal in-memory CodeModeStore for handler tests. It
// lets a test seed a continuation and script the ConsumeContinuation outcome so
// the handler's guard → protocol-error mapping can be exercised without a DB.
type fakeCodeModeStore struct {
	cont        *ifaces.CodeModeContinuation
	consumeErr  error
	consumed    bool
	executions  []*ifaces.CodeModeExecution
	putContErr  error
	deletedRows int
}

func (f *fakeCodeModeStore) PutExecution(_ context.Context, e *ifaces.CodeModeExecution) error {
	f.executions = append(f.executions, e)
	return nil
}
func (f *fakeCodeModeStore) UpdateExecutionStatus(_ context.Context, _ *ifaces.CodeModeExecution) error {
	return nil
}
func (f *fakeCodeModeStore) ListExecutions(_ context.Context, _, _ string, _ int) ([]*ifaces.CodeModeExecution, error) {
	return f.executions, nil
}
func (f *fakeCodeModeStore) PutContinuation(_ context.Context, c *ifaces.CodeModeContinuation) error {
	f.cont = c
	return f.putContErr
}
func (f *fakeCodeModeStore) ConsumeContinuation(_ context.Context, tenantID, token string, _ time.Time) (*ifaces.CodeModeContinuation, error) {
	if f.consumeErr != nil {
		return nil, f.consumeErr
	}
	if f.cont == nil || f.cont.TenantID != tenantID || f.cont.ContinuationToken != token || f.consumed {
		return nil, ifaces.ErrContinuationNotFound
	}
	f.consumed = true
	return f.cont, nil
}
func (f *fakeCodeModeStore) DeleteExpiredContinuations(_ context.Context, _ time.Time) (int, error) {
	return f.deletedRows, nil
}

func resumeArgs(token string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"continuation_token": token})
	return b
}

// TestResume_SnapshotDrift_FailsClosed: a continuation pinned to a snapshot that
// is no longer the session's active one must be rejected with snapshot_drifted —
// never replayed against different tool bindings (class C4).
func TestResume_SnapshotDrift_FailsClosed(t *testing.T) {
	sess := codeModeSession("rd1")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID) // active snapshot id = "snap-cm"
	store := &fakeCodeModeStore{cont: &ifaces.CodeModeContinuation{
		TenantID:           sess.TenantID,
		ContinuationToken:  "tok-drift",
		ExecutionID:        "exec-1",
		SessionID:          sess.ID,
		SnapshotID:         "snap-OLD", // drifted: not the session's active snapshot
		Code:               "result = 1",
		AwaitingApprovalID: "appr-1",
	}}
	d.SetCodeModeStore(store)

	_, perr := d.handleCodeModeMetaTool(context.Background(), sess,
		protocol.CallToolParams{Name: metaExecuteToolCode, Arguments: resumeArgs("tok-drift")})
	assertGuard(t, perr, reasonSnapshotDrifted)
}

func TestResume_DoubleResume_Mapped(t *testing.T) {
	sess := codeModeSession("rd2")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID)
	d.SetCodeModeStore(&fakeCodeModeStore{consumeErr: ifaces.ErrContinuationConsumed})
	_, perr := d.handleCodeModeMetaTool(context.Background(), sess,
		protocol.CallToolParams{Name: metaExecuteToolCode, Arguments: resumeArgs("tok")})
	assertGuard(t, perr, reasonDoubleResume)
}

func TestResume_Expired_Mapped(t *testing.T) {
	sess := codeModeSession("rd3")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID)
	d.SetCodeModeStore(&fakeCodeModeStore{consumeErr: ifaces.ErrContinuationExpired})
	_, perr := d.handleCodeModeMetaTool(context.Background(), sess,
		protocol.CallToolParams{Name: metaExecuteToolCode, Arguments: resumeArgs("tok")})
	assertGuard(t, perr, reasonContinuationExpired)
}

func TestResume_NotFound_Mapped(t *testing.T) {
	sess := codeModeSession("rd4")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID)
	d.SetCodeModeStore(&fakeCodeModeStore{consumeErr: ifaces.ErrContinuationNotFound})
	_, perr := d.handleCodeModeMetaTool(context.Background(), sess,
		protocol.CallToolParams{Name: metaExecuteToolCode, Arguments: resumeArgs("tok")})
	assertGuard(t, perr, reasonContinuationUnknown)
}

// TestResume_NoStore_FailsClosed: a build without a continuation store cannot
// resume — it must not silently treat the token as valid.
func TestResume_NoStore_FailsClosed(t *testing.T) {
	sess := codeModeSession("rd5")
	d := codeModeDispatcher(codeModeSnapshot(), sess.ID) // no SetCodeModeStore
	_, perr := d.handleCodeModeMetaTool(context.Background(), sess,
		protocol.CallToolParams{Name: metaExecuteToolCode, Arguments: resumeArgs("tok")})
	assertGuard(t, perr, reasonContinuationUnknown)
}

func assertGuard(t *testing.T, perr *protocol.Error, wantReason string) {
	t.Helper()
	if perr == nil {
		t.Fatalf("want guard error %q, got nil", wantReason)
	}
	if perr.Code != protocol.ErrCodeModeExecution {
		t.Errorf("guard top-level code = %d, want ErrCodeModeExecution", perr.Code)
	}
	if !containsSub(string(perr.Data), wantReason) {
		t.Errorf("guard reason %q not in error data: %s", wantReason, perr.Data)
	}
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
