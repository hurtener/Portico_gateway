package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// ToolDispatcher is the single seam through which sandbox code reaches a real
// tool. Its only production implementation is the MCP dispatcher's tools/call
// core — the exact same Go function a direct tools/call runs — so every
// in-sandbox call inherits the identical tenant/scope/policy/approval/vault/
// audit/telemetry envelope (threat model class C2, acceptance #8). There is
// deliberately no other way for a binding to dispatch: a tool call is a
// namespaced name plus JSON arguments handed to this method, and nothing else.
//
// The implementation captures the outer session identity (the session that owns
// the executeToolCode request); bindings never synthesize their own context or
// tenant. The ctx passed in is the execution span's context and must be
// propagated unchanged.
type ToolDispatcher interface {
	DispatchToolCall(ctx context.Context, namespacedTool string, argsJSON json.RawMessage) (json.RawMessage, *protocol.Error)
}

// ToolBinding maps a Starlark callable (Module.Func) to a dispatcher-namespaced
// tool name. The projector produces these from a snapshot; the runtime is
// agnostic to how the names were sanitized.
type ToolBinding struct {
	// Module is the Starlark module/variable the function hangs off, e.g.
	// "github" for github.list_issues. Conventionally the sanitized server id.
	Module string
	// Func is the Starlark function name, e.g. "list_issues". Conventionally the
	// sanitized tool short-name.
	Func string
	// NamespacedName is the dispatcher tool name, e.g. "github.list_issues".
	NamespacedName string
}

// callState tracks per-execution mutable counters shared across every binding in
// one run. Driven solely from the single interpreter goroutine, so it needs no
// locking.
type callState struct {
	toolCalls    int
	maxToolCalls int
}

// buildToolModules groups bindings by module and returns a predeclared
// StringDict mapping each module name to a frozen *starlarkstruct.Module whose
// members are the tool callables. The returned module names are also the only
// tool identifiers the static safety gate will permit as free references.
func buildToolModules(ctx context.Context, disp ToolDispatcher, bindings []ToolBinding, state *callState) (starlark.StringDict, []string) {
	byModule := map[string]starlark.StringDict{}
	for _, b := range bindings {
		if b.Module == "" || b.Func == "" || b.NamespacedName == "" {
			continue
		}
		members, ok := byModule[b.Module]
		if !ok {
			members = starlark.StringDict{}
			byModule[b.Module] = members
		}
		members[b.Func] = makeToolBuiltin(ctx, disp, b.Module, b.Func, b.NamespacedName, state)
	}

	env := starlark.StringDict{}
	names := make([]string, 0, len(byModule))
	for name, members := range byModule {
		mod := &starlarkstruct.Module{Name: name, Members: members}
		mod.Freeze()
		env[name] = mod
		names = append(names, name)
	}
	sort.Strings(names)
	return env, names
}

// makeToolBuiltin returns the Starlark callable for one tool. The closure is the
// only place sandbox code can cause a tool dispatch; it enforces the tool-call
// budget, marshals arguments, and routes through the dispatcher seam. It accepts
// keyword arguments only — tool parameters are named (matching the .pyi stubs) —
// and fails closed on a nil dispatcher.
func makeToolBuiltin(ctx context.Context, disp ToolDispatcher, module, fn, namespaced string, state *callState) *starlark.Builtin {
	qualified := module + "." + fn
	return starlark.NewBuiltin(qualified, func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) > 0 {
			return nil, &SandboxError{Code: CodeRuntimeError, Detail: qualified, Cause: fmt.Errorf("tool calls take keyword arguments only, got %d positional", len(args))}
		}
		// Budget: count the call before issuing it, fail closed past the cap.
		state.toolCalls++
		if state.toolCalls > state.maxToolCalls {
			return nil, newBudget(BudgetToolCalls)
		}
		if disp == nil {
			return nil, &SandboxError{Code: CodeToolError, Detail: namespaced, Cause: fmt.Errorf("no tool dispatcher configured")}
		}

		argsObj, err := kwargsToJSON(kwargs)
		if err != nil {
			return nil, &SandboxError{Code: CodeRuntimeError, Detail: qualified, Cause: err}
		}

		resultJSON, perr := disp.DispatchToolCall(ctx, namespaced, argsObj)
		if perr != nil {
			if perr.Code == protocol.ErrApprovalRequired {
				// The whole execution must suspend; surface a typed signal the
				// runtime intercepts to drive the continuation flow.
				return nil, &SandboxError{Code: CodeApprovalRequired, Detail: namespaced, Cause: fmt.Errorf("%s", perr.Message)}
			}
			return nil, &SandboxError{Code: CodeToolError, Detail: namespaced, Cause: fmt.Errorf("%s (code %d)", perr.Message, perr.Code)}
		}

		val, err := jsonToStarlark(resultJSON)
		if err != nil {
			return nil, &SandboxError{Code: CodeToolError, Detail: namespaced, Cause: err}
		}
		return val, nil
	})
}

// kwargsToJSON converts Starlark keyword arguments to a JSON object. Keys are
// always strings (Starlark kwargs); values go through starlarkToGo.
func kwargsToJSON(kwargs []starlark.Tuple) (json.RawMessage, error) {
	obj := make(map[string]any, len(kwargs))
	for _, kv := range kwargs {
		if len(kv) != 2 {
			return nil, fmt.Errorf("malformed keyword argument")
		}
		key, ok := starlark.AsString(kv[0])
		if !ok {
			return nil, fmt.Errorf("keyword argument name is not a string")
		}
		gv, err := starlarkToGo(kv[1])
		if err != nil {
			return nil, fmt.Errorf("argument %q: %w", key, err)
		}
		obj[key] = gv
	}
	return json.Marshal(obj)
}

// starlarkToGo converts a Starlark value to a JSON-marshalable Go value. Only
// the data types a tool argument can legitimately carry are supported; anything
// else (set, function, module, range) is rejected so it cannot smuggle a host
// handle into a tool call.
func starlarkToGo(v starlark.Value) (any, error) {
	switch t := v.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(t), nil
	case starlark.Int:
		// Preserve integer exactness by emitting a JSON number from the decimal
		// string rather than risking float64 rounding on large values.
		bi := new(big.Int)
		if _, ok := bi.SetString(t.String(), 10); !ok {
			return nil, fmt.Errorf("unrepresentable integer")
		}
		return json.Number(bi.String()), nil
	case starlark.Float:
		return float64(t), nil
	case starlark.String:
		return string(t), nil
	case *starlark.List:
		return iterableToGo(t)
	case starlark.Tuple:
		return iterableToGo(t)
	case *starlark.Dict:
		return dictToGo(t)
	default:
		return nil, fmt.Errorf("unsupported argument type %q", v.Type())
	}
}

// iterable is the subset of starlark.Iterable used for conversion.
type iterable interface {
	Iterate() starlark.Iterator
	Len() int
}

func iterableToGo(it iterable) (any, error) {
	out := make([]any, 0, it.Len())
	iter := it.Iterate()
	defer iter.Done()
	var x starlark.Value
	for iter.Next(&x) {
		gv, err := starlarkToGo(x)
		if err != nil {
			return nil, err
		}
		out = append(out, gv)
	}
	return out, nil
}

func dictToGo(d *starlark.Dict) (any, error) {
	out := make(map[string]any, d.Len())
	for _, item := range d.Items() {
		key, ok := starlark.AsString(item[0])
		if !ok {
			return nil, fmt.Errorf("dict key is not a string")
		}
		gv, err := starlarkToGo(item[1])
		if err != nil {
			return nil, err
		}
		out[key] = gv
	}
	return out, nil
}

// jsonToStarlark converts a tool result (JSON) back into an inert Starlark
// value. Numbers preserve integer-ness via a json.Number decode. The result is
// pure data: it carries no host handles and cannot trigger a tool call on its
// own (threat model class C6).
func jsonToStarlark(raw json.RawMessage) (starlark.Value, error) {
	if len(raw) == 0 {
		return starlark.None, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("decode tool result: %w", err)
	}
	return goToStarlark(v)
}

func goToStarlark(v any) (starlark.Value, error) {
	switch t := v.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(t), nil
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return starlark.MakeInt64(i), nil
		}
		// Large integer beyond int64: parse as big.Int to preserve exactness.
		if bi, ok := new(big.Int).SetString(t.String(), 10); ok {
			return starlark.MakeBigInt(bi), nil
		}
		f, err := t.Float64()
		if err != nil {
			return nil, fmt.Errorf("unrepresentable number %q", t.String())
		}
		return starlark.Float(f), nil
	case float64:
		return starlark.Float(t), nil
	case string:
		return starlark.String(t), nil
	case []any:
		elems := make([]starlark.Value, 0, len(t))
		for _, e := range t {
			sv, err := goToStarlark(e)
			if err != nil {
				return nil, err
			}
			elems = append(elems, sv)
		}
		return starlark.NewList(elems), nil
	case map[string]any:
		d := starlark.NewDict(len(t))
		// Deterministic insertion order for reproducible iteration.
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sv, err := goToStarlark(t[k])
			if err != nil {
				return nil, err
			}
			if err := d.SetKey(starlark.String(k), sv); err != nil {
				return nil, err
			}
		}
		return d, nil
	default:
		return nil, fmt.Errorf("unsupported result type %T", v)
	}
}
