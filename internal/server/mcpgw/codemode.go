package mcpgw

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/mcp/codemode/catalog"
	"github.com/hurtener/Portico_gateway/internal/mcp/codemode/runtime"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// Code Mode meta-tool names. They live under the reserved `mcp.` namespace and
// are advertised on tools/list only when the session opted into Code Mode.
// Registering any other tool under `mcp.` is forbidden (AGENTS.md §13).
const (
	metaListToolFiles   = "mcp.listToolFiles"
	metaReadToolFile    = "mcp.readToolFile"
	metaGetToolDocs     = "mcp.getToolDocs"
	metaExecuteToolCode = "mcp.executeToolCode"
)

// defaultCodeModeMaxToolCalls is the per-execution tool-call cap applied when a
// client does not specify one. Kept conservative (default-deny posture); the
// runtime enforces it when executeToolCode lands.
const defaultCodeModeMaxToolCalls = 20

// CodeModeOpts is the per-session Code Mode configuration parsed from the
// initialize capabilities. Its presence on a Session flips the tool projection
// to the meta-tools.
type CodeModeOpts struct {
	// BindingLevel controls stub granularity (server-level by default).
	BindingLevel catalog.BindingLevel
	// MaxToolCalls bounds tool calls per executeToolCode run (enforced by the
	// runtime). Parsed here so the opt-in carries it end to end.
	MaxToolCalls int
}

// extractCodeModeOpts reads the Code Mode opt-in from the client's experimental
// capabilities. The shape is
// `capabilities.experimental.portico.code_mode = {enabled, binding_level,
// max_tool_calls}` — the same `portico` experimental namespace the
// list-changed opt-in already uses. Returns nil when Code Mode is absent or
// disabled, so existing clients are unaffected (acceptance #1). Unknown binding
// levels fall back to the conservative server level.
func extractCodeModeOpts(exp map[string]json.RawMessage) *CodeModeOpts {
	raw, ok := exp["portico"]
	if !ok || len(raw) == 0 {
		return nil
	}
	var p struct {
		CodeMode *struct {
			Enabled      bool   `json:"enabled"`
			BindingLevel string `json:"binding_level"`
			MaxToolCalls int    `json:"max_tool_calls"`
		} `json:"code_mode"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil
	}
	if p.CodeMode == nil || !p.CodeMode.Enabled {
		return nil
	}
	level := catalog.BindingServer
	if p.CodeMode.BindingLevel == string(catalog.BindingTool) {
		level = catalog.BindingTool
	}
	maxCalls := p.CodeMode.MaxToolCalls
	if maxCalls <= 0 {
		maxCalls = defaultCodeModeMaxToolCalls
	}
	return &CodeModeOpts{BindingLevel: level, MaxToolCalls: maxCalls}
}

// isCodeModeMetaTool reports whether name is one of the Code Mode meta-tools.
func isCodeModeMetaTool(name string) bool {
	switch name {
	case metaListToolFiles, metaReadToolFile, metaGetToolDocs, metaExecuteToolCode:
		return true
	default:
		return false
	}
}

// codeModeMetaTools returns the meta-tool definitions advertised to a Code Mode
// session in place of the namespaced catalog. (executeToolCode is added when its
// runtime integration lands; advertising a non-functional tool would be poor
// UX, so only the implemented set is listed.)
func codeModeMetaTools() []protocol.Tool {
	return []protocol.Tool{
		{
			Name:        metaListToolFiles,
			Description: "Enumerate the virtual .pyi stub file system for this session's tool catalog.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		},
		{
			Name:        metaReadToolFile,
			Description: "Read one virtual stub file (compact function signatures for a server or tool).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"A path returned by mcp.listToolFiles, e.g. servers/github.pyi"}},"required":["path"],"additionalProperties":false}`),
		},
		{
			Name:        metaGetToolDocs,
			Description: "Fetch full docs (description, JSON Schema, risk class, approval policy) for one or more named tools.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tools":{"type":"array","items":{"type":"string"},"description":"Namespaced tool names, e.g. github.list_issues"}},"required":["tools"],"additionalProperties":false}`),
		},
		{
			Name:        metaExecuteToolCode,
			Description: "Run a Starlark snippet that calls tools through their server modules and returns a final `result`. Intermediate values stay in the sandbox; only `result` crosses back. If a called tool needs operator approval the run suspends, returning status `approval_required` plus a `continuation_token`; once approved, call again with that token to resume.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"code":{"type":"string","description":"Starlark source. Assign the final value to ` + "`result`" + `."},"continuation_token":{"type":"string","description":"Resume a suspended execution after approval. Supply this INSTEAD of code."}},"additionalProperties":false}`),
		},
	}
}

// projectCodeMode returns the projection + snapshot for the session's active
// snapshot at its binding level, going through the snapshot binder (so it is the
// session's own tenant-scoped snapshot — acceptance #10). A nil binder or
// missing snapshot yields an empty projection rather than an error, so a Code
// Mode session with no downstream servers still lists index.md.
func (d *Dispatcher) projectCodeMode(ctx context.Context, sess *Session) (catalog.Projection, *snapshots.Snapshot, *protocol.Error) {
	level := catalog.BindingServer
	if sess.CodeMode != nil {
		level = sess.CodeMode.BindingLevel
	}
	if d.snapshots == nil {
		return catalog.Project(nil, level), nil, nil
	}
	snap, err := d.snapshots.Get(ctx, sess)
	if err != nil {
		return catalog.Projection{}, nil, protocol.NewError(protocol.ErrInternalError, "code mode: snapshot bind failed", map[string]string{"error": err.Error()})
	}
	if d.codeModeCache != nil {
		return d.codeModeCache.Get(snap, level), snap, nil
	}
	return catalog.Project(snap, level), snap, nil
}

// handleCodeModeMetaTool dispatches a tools/call to one of the meta-tools.
// Tenant identity comes from the session (never from arguments), so the
// projection is always the caller's own (acceptance #10).
func (d *Dispatcher) handleCodeModeMetaTool(ctx context.Context, sess *Session, params protocol.CallToolParams) (json.RawMessage, *protocol.Error) {
	switch params.Name {
	case metaListToolFiles:
		return d.metaListToolFiles(ctx, sess)
	case metaReadToolFile:
		return d.metaReadToolFile(ctx, sess, params.Arguments)
	case metaGetToolDocs:
		return d.metaGetToolDocs(ctx, sess, params.Arguments)
	case metaExecuteToolCode:
		return d.metaExecuteToolCode(ctx, sess, params.Arguments)
	default:
		return nil, protocol.NewError(protocol.ErrToolNotEnabled, "unknown code mode meta-tool", map[string]string{"name": params.Name})
	}
}

// NewConsoleCodeModeOpts returns the default Code Mode opt-in for a synthetic,
// Console-driven session (server binding level, default tool-call cap). The REST
// layer sets it on a throwaway session to drive the meta-tools on the operator's
// behalf without a live MCP connection.
func NewConsoleCodeModeOpts() *CodeModeOpts {
	return &CodeModeOpts{BindingLevel: catalog.BindingServer, MaxToolCalls: defaultCodeModeMaxToolCalls}
}

// RunCodeModeMetaTool dispatches one Code Mode meta-tool call for a session. It
// is the exported seam the Console REST handlers call; behaviour is identical to
// an MCP tools/call of the same meta-tool (same projection, governance, audit).
func (d *Dispatcher) RunCodeModeMetaTool(ctx context.Context, sess *Session, params protocol.CallToolParams) (json.RawMessage, *protocol.Error) {
	return d.handleCodeModeMetaTool(ctx, sess, params)
}

// sessionToolDispatcher is the runtime.ToolDispatcher seam bound to one session.
// Its sole job is to forward an in-sandbox tool call to dispatchToolCall — the
// exact same governed envelope a direct tools/call runs. There is intentionally
// no other behaviour here: the sandbox cannot reach a tool except through this
// one hop into the shared dispatch path (acceptance #8). reqID is empty because
// a sandbox-issued call is not a northbound request and has nothing to cancel.
type sessionToolDispatcher struct {
	d    *Dispatcher
	sess *Session
}

func (a sessionToolDispatcher) DispatchToolCall(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, *protocol.Error) {
	return a.d.dispatchToolCall(ctx, a.sess, protocol.CallToolParams{Name: name, Arguments: args}, "")
}

// metaExecuteToolCode is the meta-tool entry point. A fresh call carries `code`
// and runs the snippet; a resume carries `continuation_token` and replays a
// suspended execution after operator approval (acceptance #9). The heavy lifting
// — sandbox execution, suspension/continuation, audit, savings — lives in
// runCodeMode / resumeCodeMode (codemode_continuation.go).
func (d *Dispatcher) metaExecuteToolCode(ctx context.Context, sess *Session, args json.RawMessage) (json.RawMessage, *protocol.Error) {
	var in struct {
		Code              string `json:"code"`
		ContinuationToken string `json:"continuation_token"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, protocol.NewError(protocol.ErrInvalidParams, "executeToolCode requires a JSON object", nil)
	}
	if in.ContinuationToken != "" {
		return d.resumeCodeMode(ctx, sess, in.ContinuationToken)
	}
	if in.Code == "" {
		return nil, protocol.NewError(protocol.ErrInvalidParams, "executeToolCode requires a non-empty 'code' argument (or a 'continuation_token' to resume)", nil)
	}
	return d.runCodeMode(ctx, sess, in.Code, nil, time.Time{}, "", false)
}

// toRuntimeBindings converts catalog ToolRefs into runtime ToolBindings.
func toRuntimeBindings(refs []catalog.ToolRef) []runtime.ToolBinding {
	out := make([]runtime.ToolBinding, 0, len(refs))
	for _, r := range refs {
		out = append(out, runtime.ToolBinding{Module: r.Module, Func: r.Func, NamespacedName: r.Namespaced})
	}
	return out
}

// codeModeErrorToProtocol maps a runtime *SandboxError onto the JSON-RPC error
// envelope. The specific code_mode.* reason travels in Data.code; the top-level
// code groups by class. An approval-required signal maps to the standard
// approval error (continuation lands in a later unit).
func codeModeErrorToProtocol(err error) *protocol.Error {
	var se *runtime.SandboxError
	if !errors.As(err, &se) {
		return protocol.NewError(protocol.ErrCodeModeExecution, "code mode execution failed", map[string]any{"error": err.Error()})
	}
	data := map[string]any{"code": se.Code}
	if se.Detail != "" {
		data["detail"] = se.Detail
	}
	switch se.Code {
	case runtime.CodeUnsafeCall:
		return protocol.NewError(protocol.ErrCodeModeUnsafe, "code mode: unsafe call", data)
	case runtime.CodeBudgetExceeded:
		return protocol.NewError(protocol.ErrCodeModeBudget, "code mode: budget exceeded", data)
	case runtime.CodeApprovalRequired:
		return protocol.NewError(protocol.ErrApprovalRequired, "approval_required", data)
	default:
		return protocol.NewError(protocol.ErrCodeModeExecution, "code mode: execution error", data)
	}
}

// codeModeErrPayload builds a redaction-safe audit payload for a failed run.
func codeModeErrPayload(err error) map[string]any {
	var se *runtime.SandboxError
	if errors.As(err, &se) {
		return map[string]any{"code": se.Code, "detail": se.Detail}
	}
	return map[string]any{"code": "code_mode.runtime_error"}
}

// emitCodeMode emits a Code Mode audit event through the dispatcher's emitter
// (nil-safe). Payloads are small and contain no tool arguments or results.
func (d *Dispatcher) emitCodeMode(ctx context.Context, sess *Session, evType string, payload map[string]any) {
	if d.emitter == nil {
		return
	}
	d.emitter.Emit(ctx, audit.Event{
		Type:       evType,
		TenantID:   sess.TenantID,
		SessionID:  sess.ID,
		UserID:     sess.UserID,
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	})
}

func (d *Dispatcher) metaListToolFiles(ctx context.Context, sess *Session) (json.RawMessage, *protocol.Error) {
	proj, _, perr := d.projectCodeMode(ctx, sess)
	if perr != nil {
		return nil, perr
	}
	paths := make([]string, 0, len(proj.Files))
	for p := range proj.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	structured, _ := json.Marshal(map[string]any{"files": paths})
	return metaResult(structured, joinLines(paths))
}

func (d *Dispatcher) metaReadToolFile(ctx context.Context, sess *Session, args json.RawMessage) (json.RawMessage, *protocol.Error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil || in.Path == "" {
		return nil, protocol.NewError(protocol.ErrInvalidParams, "readToolFile requires a 'path' argument", nil)
	}
	proj, _, perr := d.projectCodeMode(ctx, sess)
	if perr != nil {
		return nil, perr
	}
	content, ok := proj.Files[in.Path]
	if !ok {
		return nil, protocol.NewError(protocol.ErrInvalidParams, "unknown tool file", map[string]string{"path": in.Path})
	}
	structured, _ := json.Marshal(map[string]any{"path": in.Path, "content": content})
	return metaResult(structured, content)
}

// toolDoc is the full documentation surface for one tool.
type toolDoc struct {
	Name             string          `json:"name"`
	Found            bool            `json:"found"`
	ServerID         string          `json:"server_id,omitempty"`
	Description      string          `json:"description,omitempty"`
	InputSchema      json.RawMessage `json:"input_schema,omitempty"`
	RiskClass        string          `json:"risk_class,omitempty"`
	RequiresApproval bool            `json:"requires_approval,omitempty"`
	SkillID          string          `json:"skill_id,omitempty"`
}

func (d *Dispatcher) metaGetToolDocs(ctx context.Context, sess *Session, args json.RawMessage) (json.RawMessage, *protocol.Error) {
	var in struct {
		Tools []string `json:"tools"`
	}
	if err := json.Unmarshal(args, &in); err != nil || len(in.Tools) == 0 {
		return nil, protocol.NewError(protocol.ErrInvalidParams, "getToolDocs requires a non-empty 'tools' array", nil)
	}
	_, snap, perr := d.projectCodeMode(ctx, sess)
	if perr != nil {
		return nil, perr
	}
	index := map[string]snapshots.ToolInfo{}
	if snap != nil {
		for _, ti := range snap.Tools {
			index[ti.NamespacedName] = ti
		}
	}
	docs := make([]toolDoc, 0, len(in.Tools))
	for _, name := range in.Tools {
		ti, ok := index[name]
		if !ok {
			docs = append(docs, toolDoc{Name: name, Found: false})
			continue
		}
		docs = append(docs, toolDoc{
			Name:             name,
			Found:            true,
			ServerID:         ti.ServerID,
			Description:      ti.Description,
			InputSchema:      ti.InputSchema,
			RiskClass:        ti.RiskClass,
			RequiresApproval: ti.RequiresApproval,
			SkillID:          ti.SkillID,
		})
	}
	structured, _ := json.Marshal(map[string]any{"docs": docs})
	return metaResult(structured, "")
}

// metaResult builds a CallToolResult with a JSON structured payload and an
// optional text content block, then marshals it to the wire shape the
// dispatcher returns.
func metaResult(structured json.RawMessage, text string) (json.RawMessage, *protocol.Error) {
	res := protocol.CallToolResult{StructuredContent: structured}
	if text != "" {
		res.Content = []protocol.ContentBlock{{Type: "text", Text: text}}
	}
	body, err := json.Marshal(res)
	if err != nil {
		return nil, protocol.NewError(protocol.ErrInternalError, err.Error(), nil)
	}
	return body, nil
}

func joinLines(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += "\n"
		}
		out += s
	}
	return out
}
