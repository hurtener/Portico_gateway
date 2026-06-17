// Package runtime is the security-critical Starlark execution engine for Code
// Mode. It runs LLM-generated Starlark that calls real, governed tools, under a
// hardened sandbox: a static safety gate (no load/import, allowlisted built-ins
// only), four enforced execution budgets, and a single tool-call seam that
// reuses the exact MCP tools/call governance envelope.
//
// The package is written as an adversarial-input processor: every public entry
// point treats its Starlark input as hostile, fails closed on any ambiguity,
// and never panics on malformed programs. See docs/security/code-mode-threat-model.md
// for the attack classes each defense maps to.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goruntime "runtime"
	"sort"
	"time"

	starjson "go.starlark.net/lib/json"
	starmath "go.starlark.net/lib/math"
	startime "go.starlark.net/lib/time"
	"go.starlark.net/resolve"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

const sourceFilename = "codemode.star"

// allowedBuiltinNames is the exact set of Starlark built-in functions Code Mode
// exposes (threat model class C1). It is an allowlist, not a denylist: any name
// outside this set, the three stdlib modules, and the per-snapshot tool modules
// is rejected by the static gate before execution. Notably absent: load
// (statement, separately rejected), set, getattr, hasattr, dir, hash, bytes,
// chr, ord, reversed, fail, abs, tuple — minimizing surface is cheaper than
// auditing each one.
var allowedBuiltinNames = []string{
	"print", "len", "range", "enumerate", "zip", "sorted",
	"min", "max", "sum", "dict", "list", "str", "int",
	"float", "bool", "any", "all", "repr", "type",
}

// allowedConstantNames are the Universe constants (not callables) the snippet
// may reference. They are bound as predeclared so the resolver scopes them
// Predeclared rather than Universal — otherwise the Universal-scope safety gate
// would reject ordinary True/False/None literals.
var allowedConstantNames = []string{"None", "True", "False"}

// reservedModuleNames are the stdlib modules exposed to sandbox code. Tool
// module names (from the snapshot) must not collide with these.
var reservedModuleNames = []string{"json", "math", "time"}

// fileOptions configures the Starlark front-end. while/top-level control are
// enabled so the model can write useful loops; the step and wall-clock budgets
// bound any non-termination. set is disabled (not in the allowlist); recursion
// stays disabled (the default) as defense in depth, though the step budget
// would catch runaway recursion regardless.
func fileOptions() *syntax.FileOptions {
	return &syntax.FileOptions{
		Set:               false,
		While:             true,
		TopLevelControl:   true,
		GlobalReassign:    true,
		LoadBindsGlobally: false,
		Recursion:         false,
	}
}

// Redactor scrubs secrets from sandbox-visible output before it leaves the
// sandbox. The production implementation is the same audit.Redactor used for
// audit payloads, so print() output and the final result are redacted with the
// identical ruleset (threat model class C6). A nil Redactor means no redaction;
// callers that handle secrets must supply one.
type Redactor interface {
	Redact(in map[string]any) map[string]any
}

// Config parameterizes one execution. Budget, Bindings, and Dispatcher are the
// load-bearing fields; Redactor and Clock are optional.
type Config struct {
	// Budget bounds the execution. The zero Budget is normalized to defaults;
	// a zero field can never mean "unlimited".
	Budget Budget
	// Bindings are the tool callables visible to the snippet, from the snapshot
	// projector. Empty means the snippet can call no tools.
	Bindings []ToolBinding
	// Dispatcher is the single seam to the governed tools/call path. A nil
	// dispatcher means any tool call fails closed.
	Dispatcher ToolDispatcher
	// Redactor redacts print() output and the result before they leave the
	// sandbox. Nil disables redaction (use only when no secrets are possible).
	Redactor Redactor
	// Clock freezes time.now() for the execution (and, on replay, must be the
	// original timestamp so replay stays deterministic — threat model C4). Zero
	// means "now, coarsened to the second".
	Clock time.Time
	// Resume, when non-nil, replays a suspended execution: its CachedResults are
	// served for the prior calls and its ApprovalID is threaded onto the awaited
	// call. Nil is a fresh execution. On replay the caller MUST also pass the
	// original Clock so time.now() reproduces identically.
	Resume *ResumeState
}

// Result is a completed execution's output.
type Result struct {
	// Result is the snippet's `result` global, JSON-encoded. JSON null if the
	// snippet never assigned `result`.
	Result json.RawMessage
	// Output is the captured print() output, redacted and possibly truncated.
	Output string
	// OutputTruncated reports whether print output hit the byte cap.
	OutputTruncated bool
	// ToolCalls is the number of tool calls the snippet issued.
	ToolCalls int
	// Steps is the Starlark step count consumed.
	Steps uint64
	// Duration is the wall-clock execution time.
	Duration time.Duration
}

// Execute runs code under the sandbox and returns its result or a typed
// *SandboxError. It is safe to call concurrently; each call builds its own
// thread, bindings, and budgets and shares no mutable state.
func Execute(ctx context.Context, code string, cfg Config) (*Result, error) {
	budget := cfg.Budget.normalized()

	// Resolve the frozen clock ONCE, up front, so both time.now() and any
	// Suspension report the identical timestamp. A resume passes the original
	// clock back in, making time.now() replay deterministically (class C4).
	if cfg.Clock.IsZero() {
		cfg.Clock = time.Now()
	}
	cfg.Clock = cfg.Clock.Truncate(time.Second)

	opts := fileOptions()

	// 1. Parse. Syntax errors are compile errors, surfaced verbatim-but-typed.
	f, err := opts.Parse(sourceFilename, code, 0)
	if err != nil {
		return nil, &SandboxError{Code: CodeCompileError, Cause: err}
	}

	// 2. Reject load statements before resolution/compilation (class C1). load
	//    is also dead (thread.Load stays nil) but we refuse it statically so the
	//    failure is a precise unsafe_call, not a runtime "load not implemented".
	if ident := findLoadStmt(f); ident != "" {
		return nil, newUnsafe(ident)
	}

	// 3. Build the predeclared environment: allowlisted built-ins, stdlib
	//    modules, and the snapshot's tool modules. The set of names here is
	//    exactly what isPredeclared admits.
	state := &callState{maxToolCalls: budget.MaxToolCalls}
	if cfg.Resume != nil {
		state.cachedResults = cfg.Resume.CachedResults
		state.resumeApprovalID = cfg.Resume.ApprovalID
	}
	predeclared, toolModuleNames, err := buildPredeclared(ctx, cfg, state)
	if err != nil {
		return nil, err
	}

	// 4. Resolve + compile. FileProgram resolves against (isPredeclared,
	//    Universe.Has); we then statically reject any name the resolver bound to
	//    Universal scope — i.e. a real Starlark built-in we did not allowlist
	//    (set, getattr, …). Every allowlisted name resolves as Predeclared and
	//    is therefore never Universal.
	prog, err := starlark.FileProgram(f, predeclared.Has)
	if err != nil {
		// Undefined names (not predeclared, not in Universe) land here.
		return nil, &SandboxError{Code: CodeCompileError, Cause: err}
	}
	if name := firstUniversalIdent(f); name != "" {
		return nil, newUnsafe(name)
	}

	_ = toolModuleNames // names already folded into predeclared; retained for clarity

	// 5. Execute under budgets.
	return runProgram(ctx, prog, predeclared, cfg, budget, state)
}

// buildPredeclared assembles the predeclared StringDict and returns the tool
// module names. It rejects a tool module that collides with a reserved stdlib
// module name (class C5 namespace hygiene).
func buildPredeclared(ctx context.Context, cfg Config, state *callState) (starlark.StringDict, []string, error) {
	env := starlark.StringDict{}

	// Allowlisted built-ins, pulled from Universe by name (plus our own sum).
	for _, name := range allowedBuiltinNames {
		if name == "sum" {
			env[name] = starlark.NewBuiltin("sum", sumBuiltin)
			continue
		}
		v, ok := starlark.Universe[name]
		if !ok {
			return nil, nil, &SandboxError{Code: CodeRuntimeError, Cause: fmt.Errorf("internal: built-in %q missing from Universe", name)}
		}
		env[name] = v
	}

	// Universe constants (None/True/False) bound as predeclared so they are not
	// caught by the Universal-scope gate.
	for _, name := range allowedConstantNames {
		if v, ok := starlark.Universe[name]; ok {
			env[name] = v
		}
	}

	// Stdlib modules: json (encode/decode only), math (all pure), time (now,
	// frozen + coarsened).
	env["json"] = trimmedJSONModule()
	env["math"] = starmath.Module
	env["time"] = frozenTimeModule(cfg.Clock)

	// Tool modules from the snapshot.
	toolEnv, toolNames := buildToolModules(ctx, cfg.Dispatcher, cfg.Bindings, state)
	reserved := map[string]struct{}{}
	for _, n := range reservedModuleNames {
		reserved[n] = struct{}{}
	}
	for name, mod := range toolEnv {
		if _, clash := reserved[name]; clash {
			return nil, nil, &SandboxError{Code: CodeRuntimeError, Cause: fmt.Errorf("tool module %q collides with a reserved stdlib module", name)}
		}
		env[name] = mod
	}
	return env, toolNames, nil
}

// runProgram initializes the compiled program under the step budget, wall-clock
// watchdog, and output cap, then extracts the result.
func runProgram(ctx context.Context, prog *starlark.Program, predeclared starlark.StringDict, cfg Config, budget Budget, state *callState) (*Result, error) {
	out := newBoundedBuffer(budget.MaxOutputBytes)
	thread := &starlark.Thread{
		Name: "codemode",
		Load: nil, // class C1: no module loading, ever.
		Print: func(_ *starlark.Thread, msg string) {
			out.writeLine(redactLine(cfg.Redactor, msg))
		},
	}
	thread.SetMaxExecutionSteps(budget.MaxSteps)

	var stepsExceeded bool
	thread.OnMaxSteps = func(th *starlark.Thread) {
		stepsExceeded = true
		th.Cancel(BudgetSteps)
	}

	// Watchdog: cancels the interpreter on the wall-clock deadline OR when heap
	// growth exceeds the memory budget. The goroutine is always joined via done
	// (no leak — threat model C3 / goroutine hygiene). The memory sample catches
	// gradual/looping allocation bombs the step budget misses (e.g. x = x + x);
	// see classifyExecError and the threat model for the residual on a single
	// catastrophic op.
	runCtx, cancel := context.WithTimeout(ctx, budget.WallClock)
	defer cancel()
	var wallExceeded, memExceeded bool
	baseHeap := heapAlloc()
	// budget.MaxAllocBytes is positive (normalized); compare in the uint64 domain
	// the heap counters use to avoid a signed conversion of the delta.
	maxAllocBytes := uint64(budget.MaxAllocBytes) //nolint:gosec // normalized() guarantees MaxAllocBytes > 0
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(memSampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
					wallExceeded = true
				}
				thread.Cancel("context_cancelled")
				return
			case <-done:
				return
			case <-ticker.C:
				if grown := heapAlloc(); grown > baseHeap && grown-baseHeap > maxAllocBytes {
					memExceeded = true
					thread.Cancel("memory_exceeded")
					return
				}
			}
		}
	}()

	started := time.Now()
	globals, execErr := prog.Init(thread, predeclared)
	close(done)
	duration := time.Since(started)

	// Approval suspension takes precedence over every other error class: a tool
	// call returned approval_required, so the run aborted deliberately. Build the
	// continuation payload (the full prior-call prefix = cached ++ live) the
	// handler persists. Class C4.
	if state.suspended != nil {
		prefix := make([]json.RawMessage, 0, len(state.cachedResults)+len(state.liveResults))
		prefix = append(prefix, state.cachedResults...)
		prefix = append(prefix, state.liveResults...)
		return nil, &Suspension{
			CallIndex:     state.suspended.callIndex,
			ApprovalID:    state.suspended.approvalID,
			Tool:          state.suspended.tool,
			CachedResults: prefix,
			PrintBuffer:   out.String(),
			Clock:         cfg.Clock,
			Steps:         thread.ExecutionSteps(),
			ToolCalls:     state.toolCalls,
		}
	}

	if execErr != nil {
		return nil, classifyExecError(execErr, stepsExceeded, wallExceeded, memExceeded, runCtx)
	}

	resultJSON, err := extractResult(globals, cfg.Redactor)
	if err != nil {
		return nil, err
	}
	return &Result{
		Result:          resultJSON,
		Output:          out.String(),
		OutputTruncated: out.Truncated(),
		ToolCalls:       state.toolCalls,
		Steps:           thread.ExecutionSteps(),
		Duration:        duration,
	}, nil
}

// classifyExecError maps a failed Init into a typed SandboxError. Order matters:
// a typed binding error (tool error, approval-required, tool-call budget) is
// recovered first; then the budget flags; then a generic runtime error.
func classifyExecError(execErr error, stepsExceeded, wallExceeded, memExceeded bool, runCtx context.Context) error {
	var se *SandboxError
	if errors.As(execErr, &se) {
		return se
	}
	if memExceeded {
		return newBudget(BudgetMemory)
	}
	if stepsExceeded {
		return newBudget(BudgetSteps)
	}
	if wallExceeded {
		return newBudget(BudgetWallClock)
	}
	if err := runCtx.Err(); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return &SandboxError{Code: CodeRuntimeError, Cause: err}
	}
	return &SandboxError{Code: CodeRuntimeError, Cause: execErr}
}

// memSampleInterval is how often the watchdog samples the process heap. Short
// enough to catch a doubling loop before it compounds, long enough that
// ReadMemStats overhead is negligible for normal executions.
const memSampleInterval = 20 * time.Millisecond

// heapAlloc returns the current process heap-allocated bytes. It is a
// process-global figure (Go exposes no per-goroutine allocation counter), so the
// memory watchdog is a backstop, not precise accounting: under concurrent
// executions a sibling's allocation inflates the sample. This fails safe (an
// over-budget reading cancels) and is strictly better than letting one execution
// OOM-kill the whole gateway. True per-execution isolation needs an
// out-of-process sandbox (documented residual, threat model C3).
func heapAlloc() uint64 {
	var ms goruntime.MemStats
	goruntime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// extractResult reads the `result` global, redacts it, and JSON-encodes it. A
// missing result is JSON null. A non-serializable result is a runtime error.
func extractResult(globals starlark.StringDict, red Redactor) (json.RawMessage, error) {
	v, ok := globals["result"]
	if !ok || v == nil {
		return json.RawMessage("null"), nil
	}
	gv, err := starlarkToGo(v)
	if err != nil {
		return nil, &SandboxError{Code: CodeRuntimeError, Detail: "result", Cause: fmt.Errorf("result is not serializable: %w", err)}
	}
	gv = redactValue(red, gv)
	raw, err := json.Marshal(gv)
	if err != nil {
		return nil, &SandboxError{Code: CodeRuntimeError, Detail: "result", Cause: err}
	}
	return raw, nil
}

// findLoadStmt returns "load" if the file contains any load statement, else "".
func findLoadStmt(f *syntax.File) string {
	found := ""
	for _, stmt := range f.Stmts {
		syntax.Walk(stmt, func(n syntax.Node) bool {
			if _, ok := n.(*syntax.LoadStmt); ok {
				found = "load"
				return false
			}
			return true
		})
		if found != "" {
			break
		}
	}
	return found
}

// firstUniversalIdent returns the name of the first identifier the resolver
// bound to Universal scope (a real Starlark built-in we did not allowlist), or
// "" if none. Must be called after FileProgram has resolved f.
func firstUniversalIdent(f *syntax.File) string {
	found := ""
	for _, stmt := range f.Stmts {
		syntax.Walk(stmt, func(n syntax.Node) bool {
			if found != "" {
				return false
			}
			id, ok := n.(*syntax.Ident)
			if !ok {
				return true
			}
			if b, ok := id.Binding.(*resolve.Binding); ok && b.Scope == resolve.Universal {
				found = id.Name
				return false
			}
			return true
		})
		if found != "" {
			break
		}
	}
	return found
}

// redactLine applies the redactor to one print line (wrapping it so the
// map-shaped redactor can scrub it). A nil redactor is a no-op.
func redactLine(red Redactor, s string) string {
	if red == nil {
		return s
	}
	out := red.Redact(map[string]any{"v": s})
	if rv, ok := out["v"].(string); ok {
		return rv
	}
	return s
}

// redactValue applies the redactor to the result value. Map and slice shapes
// are redacted recursively by the redactor; scalars are wrapped.
func redactValue(red Redactor, v any) any {
	if red == nil {
		return v
	}
	switch m := v.(type) {
	case map[string]any:
		return red.Redact(m)
	default:
		out := red.Redact(map[string]any{"v": v})
		return out["v"]
	}
}

// trimmedJSONModule exposes only json.encode and json.decode (the plan's
// surface), both pure.
func trimmedJSONModule() *starlarkstruct.Module {
	full := starjson.Module
	m := &starlarkstruct.Module{
		Name: "json",
		Members: starlark.StringDict{
			"encode": full.Members["encode"],
			"decode": full.Members["decode"],
		},
	}
	m.Freeze()
	return m
}

// frozenTimeModule exposes only time.now(), returning a fixed timestamp
// coarsened to the second. Freezing the clock per execution blunts timing side
// channels (class C1) and makes continuation replay deterministic (class C4).
func frozenTimeModule(clock time.Time) *starlarkstruct.Module {
	if clock.IsZero() {
		clock = time.Now()
	}
	frozen := startime.Time(clock.Truncate(time.Second))
	m := &starlarkstruct.Module{
		Name: "time",
		Members: starlark.StringDict{
			"now": starlark.NewBuiltin("now", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
				if len(args) > 0 || len(kwargs) > 0 {
					return nil, fmt.Errorf("now: unexpected arguments")
				}
				return frozen, nil
			}),
		},
	}
	m.Freeze()
	return m
}

// sumBuiltin implements sum(iterable, start=0) — not present in Starlark's
// Universe but listed in the Code Mode allowlist. Pure; numeric only.
func sumBuiltin(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var iterable starlark.Iterable
	var start starlark.Value
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "iterable", &iterable, "start?", &start); err != nil {
		return nil, err
	}
	var acc starlark.Value = starlark.MakeInt(0)
	if start != nil {
		acc = start
	}
	iter := iterable.Iterate()
	defer iter.Done()
	var x starlark.Value
	for iter.Next(&x) {
		next, err := starlark.Binary(syntax.PLUS, acc, x)
		if err != nil {
			return nil, err
		}
		acc = next
	}
	return acc, nil
}

// init disables the resolver's package-level legacy globals reliance by ensuring
// our explicit FileOptions are always used. (The resolve package also exposes
// process-global flags; we never set them and never depend on them.)
func init() {
	// No global resolver flags are set here; this init exists to document that
	// Code Mode never mutates resolve.Allow* globals. Sorting the allowlist keeps
	// any future membership test deterministic.
	sort.Strings(allowedBuiltinNames)
}
