package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go.starlark.net/starlark"
)

func TestStarlarkToGo_SupportedScalars(t *testing.T) {
	cases := []struct {
		in   starlark.Value
		want any
	}{
		{starlark.None, nil},
		{starlark.Bool(true), true},
		{starlark.MakeInt(7), json.Number("7")},
		{starlark.Float(2.5), 2.5},
		{starlark.String("x"), "x"},
	}
	for _, c := range cases {
		got, err := starlarkToGo(c.in)
		if err != nil {
			t.Fatalf("%v: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("%v -> %v, want %v", c.in, got, c.want)
		}
	}
}

func TestStarlarkToGo_RejectsUnsupportedTypes(t *testing.T) {
	// A range value is not a legitimate tool argument; conversion must reject it
	// rather than smuggle a non-data value into a tool call.
	r, err := starlark.Call(&starlark.Thread{}, starlark.Universe["range"], starlark.Tuple{starlark.MakeInt(3)}, nil)
	if err != nil {
		t.Fatalf("range setup: %v", err)
	}
	if _, err := starlarkToGo(r); err == nil {
		t.Errorf("expected rejection of range value")
	}
}

func TestStarlarkToGo_TupleConverts(t *testing.T) {
	tup := starlark.Tuple{starlark.MakeInt(1), starlark.String("a")}
	got, err := starlarkToGo(tup)
	if err != nil {
		t.Fatalf("tuple: %v", err)
	}
	arr, ok := got.([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("tuple -> %v", got)
	}
}

func TestDictToGo_RejectsNonStringKey(t *testing.T) {
	d := starlark.NewDict(1)
	_ = d.SetKey(starlark.MakeInt(1), starlark.String("v"))
	if _, err := starlarkToGo(d); err == nil {
		t.Errorf("expected rejection of non-string dict key")
	}
}

func TestGoToStarlark_BigIntExact(t *testing.T) {
	v, err := jsonToStarlark(json.RawMessage(`9007199254740993`))
	if err != nil {
		t.Fatalf("bigint: %v", err)
	}
	if v.String() != "9007199254740993" {
		t.Errorf("bigint -> %s", v.String())
	}
}

func TestGoToStarlark_FloatAndNested(t *testing.T) {
	v, err := jsonToStarlark(json.RawMessage(`{"f":1.5,"a":[1,"x",null,true]}`))
	if err != nil {
		t.Fatalf("nested: %v", err)
	}
	d, ok := v.(*starlark.Dict)
	if !ok {
		t.Fatalf("want dict, got %T", v)
	}
	if d.Len() != 2 {
		t.Errorf("dict len = %d", d.Len())
	}
}

func TestJSONToStarlark_EmptyIsNone(t *testing.T) {
	v, err := jsonToStarlark(nil)
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if v != starlark.None {
		t.Errorf("empty -> %v, want None", v)
	}
}

func TestJSONToStarlark_InvalidJSON(t *testing.T) {
	if _, err := jsonToStarlark(json.RawMessage(`{not json`)); err == nil {
		t.Errorf("expected decode error")
	}
}

func TestKwargsToJSON_Empty(t *testing.T) {
	raw, err := kwargsToJSON(nil)
	if err != nil {
		t.Fatalf("empty kwargs: %v", err)
	}
	if string(raw) != "{}" {
		t.Errorf("empty kwargs -> %s, want {}", raw)
	}
}

func TestErrors_FormattingVariants(t *testing.T) {
	cases := []struct {
		err  *SandboxError
		want string
	}{
		{&SandboxError{Code: CodeUnsafeCall}, "code_mode.unsafe_call"},
		{&SandboxError{Code: CodeUnsafeCall, Detail: "load"}, "code_mode.unsafe_call (load)"},
		{&SandboxError{Code: CodeToolError, Cause: errors.New("boom")}, "code_mode.tool_error: boom"},
		{&SandboxError{Code: CodeToolError, Detail: "x", Cause: errors.New("boom")}, "code_mode.tool_error (x): boom"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("Error() = %q, want %q", got, c.want)
		}
	}
}

func TestErrors_Unwrap(t *testing.T) {
	cause := errors.New("root")
	se := &SandboxError{Code: CodeToolError, Cause: cause}
	if !errors.Is(se, cause) {
		t.Errorf("errors.Is did not traverse Unwrap")
	}
}

func TestDefaultBudget_Values(t *testing.T) {
	b := DefaultBudget()
	if b.MaxSteps != DefaultMaxSteps || b.WallClock != DefaultWallClock ||
		b.MaxOutputBytes != DefaultMaxOutputBytes || b.MaxToolCalls != DefaultMaxToolCalls {
		t.Errorf("DefaultBudget mismatch: %+v", b)
	}
}

func TestNewBoundedBuffer_NonPositiveCapDefaults(t *testing.T) {
	b := newBoundedBuffer(0)
	if b.max != DefaultMaxOutputBytes {
		t.Errorf("cap = %d, want default", b.max)
	}
}

// time.now() is frozen to the configured clock, coarsened to the second, and
// stable across calls within one execution (continuation-replay determinism).
func TestSandbox_TimeNowFrozenAndCoarsened(t *testing.T) {
	clock := time.Date(2026, 6, 17, 12, 30, 45, 123456789, time.UTC)
	code := `
a = time.now()
b = time.now()
result = str(a) == str(b)
`
	res, err := Execute(context.Background(), code, Config{Clock: clock})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(res.Result) != "true" {
		t.Errorf("time.now() not stable within execution: %s", res.Result)
	}
}

func TestSandbox_ToolModuleCollidesWithStdlib(t *testing.T) {
	// A snapshot that produced a tool module named "json" must be rejected, not
	// silently shadow the stdlib module.
	_, err := Execute(context.Background(), `result = 1`, Config{
		Bindings:   []ToolBinding{{Module: "json", Func: "x", NamespacedName: "json.x"}},
		Dispatcher: &mockDispatcher{},
	})
	var se *SandboxError
	if !asSandbox(err, &se) || se.Code != CodeRuntimeError {
		t.Fatalf("want runtime_error on stdlib collision, got %v", err)
	}
	if !strings.Contains(se.Error(), "collides") {
		t.Errorf("error should mention collision: %v", se)
	}
}

func TestSandbox_NonSerializableResultRejected(t *testing.T) {
	// A function value cannot cross back as result.
	code := `
def f():
    return 1
result = f
`
	_, err := Execute(context.Background(), code, Config{})
	var se *SandboxError
	if !asSandbox(err, &se) || se.Code != CodeRuntimeError {
		t.Fatalf("want runtime_error for non-serializable result, got %v", err)
	}
}

func TestSandbox_SumWithStart(t *testing.T) {
	res, err := Execute(context.Background(), `result = sum([1, 2, 3], 10)`, Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(res.Result) != "16" {
		t.Errorf("sum with start = %s, want 16", res.Result)
	}
}
