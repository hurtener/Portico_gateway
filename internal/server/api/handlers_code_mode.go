package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/hurtener/Portico_gateway/internal/auth/scope"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
)

// defaultCodeModeListLimit caps an unbounded /api/code-mode/executions list.
const defaultCodeModeListLimit = 100

// CodeModeExecutionItem is one row for GET /api/code-mode/executions. It carries
// no snippet or tool arguments — only the SHA, counts, and the savings estimate.
type CodeModeExecutionItem struct {
	ExecutionID    string `json:"execution_id"`
	SessionID      string `json:"session_id"`
	Status         string `json:"status"`
	StartedAt      string `json:"started_at"`
	FinishedAt     string `json:"finished_at,omitempty"`
	SnippetSHA     string `json:"snippet_sha"`
	ToolCalls      int    `json:"tool_calls"`
	TokensSavedEst int    `json:"tokens_saved_est"`
}

// CodeModeSummaryResponse is the dashboard ROI rollup for
// GET /api/code-mode/savings.
type CodeModeSummaryResponse struct {
	Executions     int            `json:"executions"`
	ToolCalls      int            `json:"tool_calls"`
	TokensSavedEst int            `json:"tokens_saved_est"`
	ByStatus       map[string]int `json:"by_status"`
	Since          string         `json:"since,omitempty"`
}

// requireCodeModeRead gates the Code Mode observability endpoints. They expose a
// tenant's execution history, so admin scope is required.
func requireCodeModeRead(w http.ResponseWriter, id tenant.Identity) bool {
	if scope.Has(id, "admin") {
		return true
	}
	writeJSONError(w, http.StatusForbidden, "forbidden", "code mode observability requires admin scope", nil)
	return false
}

// runConsoleCodeMode drives a Code Mode meta-tool through a synthetic,
// Console-owned session for the tenant (so the operator can list files / read
// stubs / run a snippet without a live MCP connection). It writes the meta-tool
// result's structured content on success, or maps the typed Code Mode error.
// Requires the dispatcher + session registry to be wired (else 503).
func runConsoleCodeMode(d Deps, w http.ResponseWriter, r *http.Request, params protocol.CallToolParams) {
	if d.Dispatcher == nil || d.Sessions == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "code_mode_not_configured", "code mode runtime not wired in this build", nil)
		return
	}
	id := tenant.MustFrom(r.Context())
	if !requireCodeModeRead(w, id) {
		return
	}
	sess := d.Sessions.Create(id.TenantID, id.UserID, "")
	sess.CodeMode = mcpgw.NewConsoleCodeModeOpts()
	defer d.Sessions.Close(sess.ID)

	body, perr := d.Dispatcher.RunCodeModeMetaTool(r.Context(), sess, params)
	if perr != nil {
		writeCodeModeProtocolError(w, perr)
		return
	}
	var res protocol.CallToolResult
	if err := json.Unmarshal(body, &res); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "decode_failed", err.Error(), nil)
		return
	}
	if len(res.StructuredContent) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(res.StructuredContent)
}

// writeCodeModeProtocolError maps a meta-tool *protocol.Error onto HTTP. Code
// Mode failures (unsafe / budget / execution) are client errors (400) carrying
// the specific code_mode.* reason in the body; anything else is a 500.
func writeCodeModeProtocolError(w http.ResponseWriter, perr *protocol.Error) {
	status := http.StatusInternalServerError
	switch perr.Code {
	case protocol.ErrCodeModeUnsafe, protocol.ErrCodeModeBudget, protocol.ErrCodeModeExecution,
		protocol.ErrInvalidParams, protocol.ErrApprovalRequired:
		status = http.StatusBadRequest
	}
	var data map[string]any
	if len(perr.Data) > 0 {
		_ = json.Unmarshal(perr.Data, &data)
	}
	writeJSONError(w, status, "code_mode_error", perr.Message, data)
}

// runCodeModeSnippetHandler handles POST /api/code-mode/run {code} — runs a
// Starlark snippet against a synthetic Console code-mode session.
func runCodeModeSnippetHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Code == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "a non-empty 'code' field is required", nil)
			return
		}
		args, _ := json.Marshal(map[string]string{"code": in.Code})
		runConsoleCodeMode(d, w, r, protocol.CallToolParams{Name: "mcp.executeToolCode", Arguments: args})
	}
}

// listCodeModeFilesHandler handles GET /api/code-mode/files — the virtual stub
// file list for the tenant's snapshot (drives the Playground file tree).
func listCodeModeFilesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		runConsoleCodeMode(d, w, r, protocol.CallToolParams{Name: "mcp.listToolFiles"})
	}
}

// readCodeModeFileHandler handles GET /api/code-mode/files/read?path= — one stub
// file's content.
func readCodeModeFileHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "a 'path' query parameter is required", nil)
			return
		}
		args, _ := json.Marshal(map[string]string{"path": path})
		runConsoleCodeMode(d, w, r, protocol.CallToolParams{Name: "mcp.readToolFile", Arguments: args})
	}
}

// listCodeModeExecutionsHandler handles GET /api/code-mode/executions?session=&limit=.
func listCodeModeExecutionsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.CodeMode == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "code_mode_not_configured", "code mode store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireCodeModeRead(w, id) {
			return
		}
		limit := defaultCodeModeListLimit
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		execs, err := d.CodeMode.ListExecutions(r.Context(), id.TenantID, r.URL.Query().Get("session"), limit)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]CodeModeExecutionItem, 0, len(execs))
		for _, e := range execs {
			out = append(out, CodeModeExecutionItem{
				ExecutionID:    e.ExecutionID,
				SessionID:      e.SessionID,
				Status:         e.Status,
				StartedAt:      e.StartedAt,
				FinishedAt:     e.FinishedAt,
				SnippetSHA:     e.SnippetSHA,
				ToolCalls:      e.ToolCalls,
				TokensSavedEst: e.TokensSavedEst,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// codeModeSavingsHandler handles GET /api/code-mode/savings?since=RFC3339 —
// the tokens-saved ROI rollup for the dashboard.
func codeModeSavingsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.CodeMode == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "code_mode_not_configured", "code mode store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireCodeModeRead(w, id) {
			return
		}
		since := r.URL.Query().Get("since")
		sum, err := d.CodeMode.SummarizeExecutions(r.Context(), id.TenantID, since)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "summarize_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, CodeModeSummaryResponse{
			Executions:     sum.Executions,
			ToolCalls:      sum.ToolCalls,
			TokensSavedEst: sum.TokensSavedEst,
			ByStatus:       sum.ByStatus,
			Since:          since,
		})
	}
}
