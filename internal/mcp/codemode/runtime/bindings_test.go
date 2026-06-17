package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// mockDispatcher records every call and returns scripted results. It is the
// stand-in for the real governed tools/call core in unit tests; the integration
// test that proves the real envelope lives in test/integration/codemode.
type mockDispatcher struct {
	calls   []dispatchCall
	result  json.RawMessage
	perr    *protocol.Error
	gotCtx  context.Context
	ctxProb any
}

type dispatchCall struct {
	name string
	args json.RawMessage
}

type ctxKey string

func (m *mockDispatcher) DispatchToolCall(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, *protocol.Error) {
	m.calls = append(m.calls, dispatchCall{name: name, args: args})
	m.gotCtx = ctx
	m.ctxProb = ctx.Value(ctxKey("probe"))
	if m.perr != nil {
		return nil, m.perr
	}
	if m.result == nil {
		return json.RawMessage("null"), nil
	}
	return m.result, nil
}

func twoToolBindings() []ToolBinding {
	return []ToolBinding{
		{Module: "github", Func: "list_issues", NamespacedName: "github.list_issues"},
		{Module: "github", Func: "comment_on", NamespacedName: "github.comment_on"},
		{Module: "jira", Func: "create", NamespacedName: "jira.create"},
	}
}

func TestBindings_HappyPath_DispatchesNamespacedNameAndArgs(t *testing.T) {
	disp := &mockDispatcher{result: json.RawMessage(`{"issues":[1,2,3],"ok":true}`)}
	res, err := Execute(context.Background(), `result = github.list_issues(repo="owner/r", state="open")`, Config{
		Bindings:   twoToolBindings(),
		Dispatcher: disp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(disp.calls) != 1 {
		t.Fatalf("want 1 dispatch, got %d", len(disp.calls))
	}
	if disp.calls[0].name != "github.list_issues" {
		t.Errorf("namespaced name = %q, want github.list_issues", disp.calls[0].name)
	}
	var gotArgs map[string]any
	if e := json.Unmarshal(disp.calls[0].args, &gotArgs); e != nil {
		t.Fatalf("args not json: %v", e)
	}
	want := map[string]any{"repo": "owner/r", "state": "open"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("args = %v, want %v", gotArgs, want)
	}
	// Result must round-trip back as the snippet's `result`.
	var got map[string]any
	if e := json.Unmarshal(res.Result, &got); e != nil {
		t.Fatalf("result not json: %v", e)
	}
	if got["ok"] != true {
		t.Errorf("result lost data: %v", got)
	}
	if res.ToolCalls != 1 {
		t.Errorf("ToolCalls = %d, want 1", res.ToolCalls)
	}
}

// C2: the dispatcher context is the outer execution context, not a synthesized
// one — proven by a probe value that must survive into the dispatcher.
func TestBindings_ContextPropagatedFromOuterExecution(t *testing.T) {
	disp := &mockDispatcher{result: json.RawMessage(`1`)}
	ctx := context.WithValue(context.Background(), ctxKey("probe"), "tenant-A-span")
	_, err := Execute(ctx, `result = jira.create(summary="x")`, Config{
		Bindings:   twoToolBindings(),
		Dispatcher: disp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if disp.ctxProb != "tenant-A-span" {
		t.Fatalf("binding synthesized its own context; probe = %v, want tenant-A-span", disp.ctxProb)
	}
}

// C2: with no dispatcher there is no path to a tool — it fails closed.
func TestBindings_NilDispatcherFailsClosed(t *testing.T) {
	_, err := Execute(context.Background(), `result = github.list_issues(repo="x")`, Config{
		Bindings:   twoToolBindings(),
		Dispatcher: nil,
	})
	assertSandboxCode(t, err, CodeToolError)
}

// C2: a policy denial from the envelope aborts the snippet as a tool error; it
// does not silently succeed.
func TestBindings_PolicyDenyBecomesToolError(t *testing.T) {
	disp := &mockDispatcher{perr: protocol.NewError(protocol.ErrPolicyDenied, "policy denied", nil)}
	_, err := Execute(context.Background(), `result = github.list_issues(repo="x")`, Config{
		Bindings:   twoToolBindings(),
		Dispatcher: disp,
	})
	assertSandboxCode(t, err, CodeToolError)
}

// C2/C4: an approval-required result surfaces as the typed approval signal the
// runtime intercepts to drive the continuation flow.
func TestBindings_ApprovalRequiredSuspendsExecution(t *testing.T) {
	disp := &mockDispatcher{perr: protocol.NewError(protocol.ErrApprovalRequired, "approval_required",
		map[string]any{"approval_id": "appr-1"})}
	_, err := Execute(context.Background(), `result = github.comment_on(repo="x", issue=1, body="hi")`, Config{
		Bindings:   twoToolBindings(),
		Dispatcher: disp,
	})
	// An in-sandbox approval_required no longer surfaces as a raw SandboxError —
	// it suspends the whole execution and returns a *Suspension carrying the
	// continuation payload (the awaited call is the very first one, idx 0).
	var susp *Suspension
	if !errors.As(err, &susp) {
		t.Fatalf("want *Suspension, got %T: %v", err, err)
	}
	if susp.CallIndex != 0 || susp.ApprovalID != "appr-1" || susp.Tool != "github.comment_on" {
		t.Errorf("suspension fields wrong: %+v", susp)
	}
	if len(susp.CachedResults) != 0 {
		t.Errorf("no calls completed before the awaited one; CachedResults should be empty, got %d", len(susp.CachedResults))
	}
}

func TestBindings_PositionalArgsRejected(t *testing.T) {
	disp := &mockDispatcher{result: json.RawMessage(`1`)}
	_, err := Execute(context.Background(), `result = github.list_issues("owner/r")`, Config{
		Bindings:   twoToolBindings(),
		Dispatcher: disp,
	})
	assertSandboxCode(t, err, CodeRuntimeError)
	if len(disp.calls) != 0 {
		t.Errorf("dispatcher should not have been called on a positional-arg rejection")
	}
}

func TestBindings_UnboundModuleRejectedBeforeDispatch(t *testing.T) {
	disp := &mockDispatcher{result: json.RawMessage(`1`)}
	// "slack" is not in the bindings, so it must never reach the dispatcher.
	_, err := Execute(context.Background(), `result = slack.post(channel="x")`, Config{
		Bindings:   twoToolBindings(),
		Dispatcher: disp,
	})
	if err == nil {
		t.Fatal("expected rejection for unbound module")
	}
	if len(disp.calls) != 0 {
		t.Errorf("unbound module reached dispatcher: %v", disp.calls)
	}
}

// Value conversion round-trips: Starlark args -> JSON must preserve types.
func TestKwargsToJSON_PreservesTypes(t *testing.T) {
	disp := &mockDispatcher{result: json.RawMessage(`null`)}
	code := `result = jira.create(s="text", n=42, f=3.5, b=True, none=None, lst=[1,2], obj={"k": "v"})`
	_, err := Execute(context.Background(), code, Config{Bindings: twoToolBindings(), Dispatcher: disp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]any
	if e := json.Unmarshal(disp.calls[0].args, &got); e != nil {
		t.Fatalf("args not json: %v", e)
	}
	if got["s"] != "text" {
		t.Errorf("string lost: %v", got["s"])
	}
	if got["n"].(float64) != 42 {
		t.Errorf("int lost: %v", got["n"])
	}
	if got["f"].(float64) != 3.5 {
		t.Errorf("float lost: %v", got["f"])
	}
	if got["b"] != true {
		t.Errorf("bool lost: %v", got["b"])
	}
	if got["none"] != nil {
		t.Errorf("none lost: %v", got["none"])
	}
	if !reflect.DeepEqual(got["lst"], []any{float64(1), float64(2)}) {
		t.Errorf("list lost: %v", got["lst"])
	}
	if !reflect.DeepEqual(got["obj"], map[string]any{"k": "v"}) {
		t.Errorf("dict lost: %v", got["obj"])
	}
}

// Integer exactness: a large int argument must not be rounded through float64.
func TestKwargsToJSON_LargeIntExact(t *testing.T) {
	disp := &mockDispatcher{result: json.RawMessage(`null`)}
	_, err := Execute(context.Background(), `result = jira.create(big=9007199254740993)`, Config{
		Bindings:   twoToolBindings(),
		Dispatcher: disp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 9007199254740993 = 2^53 + 1 is not representable as float64; the raw JSON
	// must contain the exact digits.
	if got := string(disp.calls[0].args); got != `{"big":9007199254740993}` {
		t.Errorf("large int not exact: %s", got)
	}
}

func TestJSONToStarlark_RoundTripThroughResult(t *testing.T) {
	// A tool returns nested JSON; the snippet returns it as result unchanged.
	disp := &mockDispatcher{result: json.RawMessage(`{"a":1,"b":[true,null,"x"],"c":{"d":2.5}}`)}
	res, err := Execute(context.Background(), `result = github.list_issues(repo="x")`, Config{
		Bindings:   twoToolBindings(),
		Dispatcher: disp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]any
	if e := json.Unmarshal(res.Result, &got); e != nil {
		t.Fatalf("result not json: %v", e)
	}
	want := map[string]any{
		"a": float64(1),
		"b": []any{true, nil, "x"},
		"c": map[string]any{"d": 2.5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip lost data:\n got %v\nwant %v", got, want)
	}
}

// assertSandboxCode fails the test unless err is a *SandboxError with the given
// Code.
func assertSandboxCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %s, got nil", code)
	}
	var se *SandboxError
	if !errors.As(err, &se) {
		t.Fatalf("error is not *SandboxError: %T %v", err, err)
	}
	if se.Code != code {
		t.Fatalf("error code = %q (detail %q), want %q", se.Code, se.Detail, code)
	}
}
