package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// stubCodeModeStore implements just the read methods the handlers use; the
// write/continuation methods are no-ops (the API never calls them).
type stubCodeModeStore struct {
	execs    []*ifaces.CodeModeExecution
	summary  *ifaces.CodeModeSummary
	failList bool
	failSum  bool
}

func (s *stubCodeModeStore) ListExecutions(_ context.Context, tenantID, sessionID string, _ int) ([]*ifaces.CodeModeExecution, error) {
	if s.failList {
		return nil, errors.New("boom")
	}
	out := []*ifaces.CodeModeExecution{}
	for _, e := range s.execs {
		if e.TenantID == tenantID && (sessionID == "" || e.SessionID == sessionID) {
			out = append(out, e)
		}
	}
	return out, nil
}
func (s *stubCodeModeStore) SummarizeExecutions(_ context.Context, _, _ string) (*ifaces.CodeModeSummary, error) {
	if s.failSum {
		return nil, errors.New("boom")
	}
	return s.summary, nil
}
func (s *stubCodeModeStore) PutExecution(context.Context, *ifaces.CodeModeExecution) error {
	return nil
}
func (s *stubCodeModeStore) UpdateExecutionStatus(context.Context, *ifaces.CodeModeExecution) error {
	return nil
}
func (s *stubCodeModeStore) PutContinuation(context.Context, *ifaces.CodeModeContinuation) error {
	return nil
}
func (s *stubCodeModeStore) ConsumeContinuation(context.Context, string, string, time.Time) (*ifaces.CodeModeContinuation, error) {
	return nil, ifaces.ErrContinuationNotFound
}
func (s *stubCodeModeStore) DeleteExpiredContinuations(context.Context, time.Time) (int, error) {
	return 0, nil
}

func TestListCodeModeExecutions_Happy(t *testing.T) {
	d := Deps{CodeMode: &stubCodeModeStore{execs: []*ifaces.CodeModeExecution{
		{TenantID: "t1", ExecutionID: "e1", SessionID: "s1", Status: "completed", ToolCalls: 3, TokensSavedEst: 1280},
		{TenantID: "other", ExecutionID: "e2", SessionID: "s9"},
	}}}
	w := runHandler(listCodeModeExecutionsHandler(d), newReq("GET", "/api/code-mode/executions", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var out []CodeModeExecutionItem
	decodeJSON(t, w, &out)
	if len(out) != 1 || out[0].ExecutionID != "e1" || out[0].TokensSavedEst != 1280 {
		t.Fatalf("tenant-scoped list wrong: %+v", out)
	}
}

func TestListCodeModeExecutions_NilStore503(t *testing.T) {
	w := runHandler(listCodeModeExecutionsHandler(Deps{}), newReq("GET", "/api/code-mode/executions", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestListCodeModeExecutions_NonAdmin403(t *testing.T) {
	d := Deps{CodeMode: &stubCodeModeStore{}}
	w := runHandler(listCodeModeExecutionsHandler(d), newReq("GET", "/api/code-mode/executions", nil, "llm:invoke"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for non-admin", w.Code)
	}
}

func TestCodeModeSavings_Happy(t *testing.T) {
	d := Deps{CodeMode: &stubCodeModeStore{summary: &ifaces.CodeModeSummary{
		Executions: 5, ToolCalls: 12, TokensSavedEst: 9001, ByStatus: map[string]int{"completed": 4, "failed": 1},
	}}}
	w := runHandler(codeModeSavingsHandler(d), newReq("GET", "/api/code-mode/savings", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var out CodeModeSummaryResponse
	decodeJSON(t, w, &out)
	if out.Executions != 5 || out.TokensSavedEst != 9001 || out.ByStatus["completed"] != 4 {
		t.Fatalf("summary wrong: %+v", out)
	}
}

func TestCodeModeSavings_StoreError500(t *testing.T) {
	d := Deps{CodeMode: &stubCodeModeStore{failSum: true}}
	w := runHandler(codeModeSavingsHandler(d), newReq("GET", "/api/code-mode/savings", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}
