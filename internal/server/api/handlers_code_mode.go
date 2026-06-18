package api

import (
	"net/http"
	"strconv"

	"github.com/hurtener/Portico_gateway/internal/auth/scope"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
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
