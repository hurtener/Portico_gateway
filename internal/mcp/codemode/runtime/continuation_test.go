package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// approvalDispatcher gates a chosen tool behind approval until a resume approval
// id is threaded onto the call's context, then returns a real result. It records
// every REAL dispatch (cached replays never reach it) and the approval id seen
// on each, so tests can prove the no-double-dispatch and id-threading invariants.
type approvalDispatcher struct {
	realCalls   []dispatchCall
	threadedIDs []string
	gateTools   map[string]string // tool -> approval id to issue while ungated
}

func (d *approvalDispatcher) DispatchToolCall(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, *protocol.Error) {
	id := ResumeApprovalIDFrom(ctx)
	d.realCalls = append(d.realCalls, dispatchCall{name: name, args: args})
	d.threadedIDs = append(d.threadedIDs, id)
	if apprID, gated := d.gateTools[name]; gated && id == "" {
		return nil, protocol.NewError(protocol.ErrApprovalRequired, "approval_required",
			map[string]any{"approval_id": apprID, "tool": name})
	}
	return json.RawMessage(fmt.Sprintf(`{"tool":%q,"n":%d}`, name, len(d.realCalls))), nil
}

const twoCallSnippet = `
a = github.list_issues(repo="r")
b = github.comment_on(repo="r", issue=1, body="hi")
result = {"a": a, "b": b}
`

func TestContinuation_SuspendsOnApprovalRequired(t *testing.T) {
	disp := &approvalDispatcher{gateTools: map[string]string{"github.comment_on": "appr-xyz"}}
	_, err := Execute(context.Background(), twoCallSnippet, Config{
		Bindings:   twoToolBindings(),
		Dispatcher: disp,
	})
	var susp *Suspension
	if !errors.As(err, &susp) {
		t.Fatalf("want *Suspension, got %T: %v", err, err)
	}
	if susp.CallIndex != 1 {
		t.Errorf("CallIndex = %d, want 1", susp.CallIndex)
	}
	if susp.ApprovalID != "appr-xyz" {
		t.Errorf("ApprovalID = %q, want appr-xyz", susp.ApprovalID)
	}
	if susp.Tool != "github.comment_on" {
		t.Errorf("Tool = %q", susp.Tool)
	}
	if len(susp.CachedResults) != 1 {
		t.Fatalf("CachedResults len = %d, want 1 (only the completed first call)", len(susp.CachedResults))
	}
	if susp.Clock.IsZero() {
		t.Error("Clock must be set so resume replays time.now() deterministically")
	}
	// The dispatcher saw both calls: the first completed, the second gated.
	if len(disp.realCalls) != 2 {
		t.Fatalf("real dispatches = %d, want 2", len(disp.realCalls))
	}
}

func TestContinuation_ResumesWithCachedResults(t *testing.T) {
	disp := &approvalDispatcher{gateTools: map[string]string{"github.comment_on": "appr-xyz"}}
	_, err := Execute(context.Background(), twoCallSnippet, Config{Bindings: twoToolBindings(), Dispatcher: disp})
	var susp *Suspension
	if !errors.As(err, &susp) {
		t.Fatalf("want suspension, got %v", err)
	}

	// Resume with a FRESH dispatcher: the first call must be served from cache
	// (never re-dispatched — no double side effect), only the awaited call runs.
	resumeDisp := &approvalDispatcher{gateTools: map[string]string{"github.comment_on": "appr-xyz"}}
	res, err := Execute(context.Background(), twoCallSnippet, Config{
		Bindings:   twoToolBindings(),
		Dispatcher: resumeDisp,
		Clock:      susp.Clock,
		Resume:     &ResumeState{CachedResults: susp.CachedResults, ApprovalID: susp.ApprovalID},
	})
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if len(resumeDisp.realCalls) != 1 {
		t.Fatalf("resume dispatched %d calls, want 1 (cached prior call must not re-dispatch)", len(resumeDisp.realCalls))
	}
	if resumeDisp.realCalls[0].name != "github.comment_on" {
		t.Errorf("resume re-dispatched %q, want the awaited github.comment_on", resumeDisp.realCalls[0].name)
	}
	if resumeDisp.threadedIDs[0] != "appr-xyz" {
		t.Errorf("awaited call ctx approval id = %q, want appr-xyz", resumeDisp.threadedIDs[0])
	}
	// The merged result carries both the cached (a) and freshly approved (b) call.
	var out struct {
		A map[string]any `json:"a"`
		B map[string]any `json:"b"`
	}
	if jerr := json.Unmarshal(res.Result, &out); jerr != nil {
		t.Fatalf("result decode: %v (%s)", jerr, res.Result)
	}
	if out.A["tool"] != "github.list_issues" || out.B["tool"] != "github.comment_on" {
		t.Errorf("merged result wrong: %s", res.Result)
	}
}

func TestContinuation_FrozenClockReplaysDeterministically(t *testing.T) {
	clock := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	disp := &approvalDispatcher{gateTools: map[string]string{}}
	code := `result = str(time.now())`
	run := func() string {
		r, err := Execute(context.Background(), code, Config{Bindings: twoToolBindings(), Dispatcher: disp, Clock: clock})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		return string(r.Result)
	}
	if a, b := run(), run(); a != b {
		t.Errorf("time.now() not deterministic across runs with a fixed clock: %s vs %s", a, b)
	}
}

func TestContinuation_ChainedSuspends_ExtendCache(t *testing.T) {
	// Two distinct gated tools: comment_on, then jira.create. The first resume
	// approves comment_on but trips on jira.create → a second suspension whose
	// cache now holds BOTH prior results.
	code := `
a = github.list_issues(repo="r")
b = github.comment_on(repo="r", issue=1, body="hi")
c = jira.create(summary="s")
result = {"a": a, "b": b, "c": c}
`
	gates := func() map[string]string {
		return map[string]string{"github.comment_on": "appr-1", "jira.create": "appr-2"}
	}
	d1 := &approvalDispatcher{gateTools: gates()}
	_, err := Execute(context.Background(), code, Config{Bindings: twoToolBindings(), Dispatcher: d1})
	var s1 *Suspension
	if !errors.As(err, &s1) || s1.CallIndex != 1 || s1.ApprovalID != "appr-1" {
		t.Fatalf("first suspend wrong: %+v err=%v", s1, err)
	}

	// Resume 1: comment_on approved (id threaded), jira.create now gates.
	d2 := &approvalDispatcher{gateTools: gates()}
	_, err = Execute(context.Background(), code, Config{
		Bindings: twoToolBindings(), Dispatcher: d2, Clock: s1.Clock,
		Resume: &ResumeState{CachedResults: s1.CachedResults, ApprovalID: s1.ApprovalID},
	})
	var s2 *Suspension
	if !errors.As(err, &s2) {
		t.Fatalf("want second suspension, got %v", err)
	}
	if s2.CallIndex != 2 || s2.ApprovalID != "appr-2" || s2.Tool != "jira.create" {
		t.Errorf("second suspend wrong: idx=%d id=%q tool=%q", s2.CallIndex, s2.ApprovalID, s2.Tool)
	}
	if len(s2.CachedResults) != 2 {
		t.Fatalf("chained cache len = %d, want 2 (list_issues + comment_on)", len(s2.CachedResults))
	}

	// Resume 2: jira.create approved → completes.
	d3 := &approvalDispatcher{gateTools: gates()}
	res, err := Execute(context.Background(), code, Config{
		Bindings: twoToolBindings(), Dispatcher: d3, Clock: s2.Clock,
		Resume: &ResumeState{CachedResults: s2.CachedResults, ApprovalID: s2.ApprovalID},
	})
	if err != nil {
		t.Fatalf("final resume failed: %v", err)
	}
	if len(d3.realCalls) != 1 || d3.realCalls[0].name != "jira.create" {
		t.Fatalf("final resume should dispatch only jira.create, got %+v", d3.realCalls)
	}
	if len(res.Result) == 0 {
		t.Error("final result empty")
	}
}

func TestContinuation_BudgetStillBoundsReplay(t *testing.T) {
	// A resume with a cached prefix longer than the tool-call budget must still
	// fail closed on the budget — replay does not get a free pass.
	disp := &approvalDispatcher{gateTools: map[string]string{}}
	cached := []json.RawMessage{json.RawMessage(`{"x":1}`), json.RawMessage(`{"x":2}`)}
	code := `
a = github.list_issues(repo="r")
b = github.comment_on(repo="r", issue=1, body="hi")
result = {"a": a, "b": b}
`
	_, err := Execute(context.Background(), code, Config{
		Bindings:   twoToolBindings(),
		Dispatcher: disp,
		Budget:     Budget{MaxToolCalls: 1},
		Resume:     &ResumeState{CachedResults: cached},
	})
	var se *SandboxError
	if !errors.As(err, &se) || se.Code != CodeBudgetExceeded {
		t.Fatalf("want budget exceeded on replay, got %v", err)
	}
}
