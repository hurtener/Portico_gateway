package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// stubPlaygroundController implements PlaygroundController for handler
// tests without dragging the full playground service in.
type stubPlaygroundController struct {
	startErr      error
	endErr        error
	catalogErr    error
	callErr       error
	streamErr     error
	correlateErr  error
	replayErr     error
	lastCallReq   PlaygroundCallRequest
	streamFrames  []PlaygroundStreamFrame
	storedSession *PlaygroundSessionDTO
}

func (s *stubPlaygroundController) StartSession(_ context.Context, req PlaygroundStartSessionRequest) (*PlaygroundSessionDTO, error) {
	if s.startErr != nil {
		return nil, s.startErr
	}
	now := time.Now().UTC()
	dto := &PlaygroundSessionDTO{
		ID:        "psn_test",
		TenantID:  req.TenantID,
		Token:     "tok",
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now,
	}
	s.storedSession = dto
	return dto, nil
}

func (s *stubPlaygroundController) EndSession(_ context.Context, _ string) error {
	return s.endErr
}

func (s *stubPlaygroundController) GetSession(_ string) *PlaygroundSessionDTO {
	return s.storedSession
}

func (s *stubPlaygroundController) Catalog(_ context.Context, _ string) (*PlaygroundCatalogDTO, error) {
	if s.catalogErr != nil {
		return nil, s.catalogErr
	}
	return &PlaygroundCatalogDTO{SnapshotID: "snap-1", Catalog: map[string]any{"servers": []any{}}}, nil
}

func (s *stubPlaygroundController) IssueCall(_ context.Context, sid string, req PlaygroundCallRequest) (*PlaygroundCallEnvelope, error) {
	s.lastCallReq = req
	if s.callErr != nil {
		return nil, s.callErr
	}
	return &PlaygroundCallEnvelope{CallID: "call_1", SessionID: sid, Status: "enqueued"}, nil
}

func (s *stubPlaygroundController) StreamCall(ctx context.Context, _, _ string) (<-chan PlaygroundStreamFrame, error) {
	if s.streamErr != nil {
		return nil, s.streamErr
	}
	out := make(chan PlaygroundStreamFrame, len(s.streamFrames)+1)
	for _, f := range s.streamFrames {
		out <- f
	}
	out <- PlaygroundStreamFrame{Type: "end"}
	close(out)
	_ = ctx
	return out, nil
}

func (s *stubPlaygroundController) Correlation(_ context.Context, _ string, _ time.Time) (any, error) {
	if s.correlateErr != nil {
		return nil, s.correlateErr
	}
	return map[string]any{"spans": []any{}, "audits": []any{}, "policy": []any{}, "drift": []any{}}, nil
}

func (s *stubPlaygroundController) RunCorrelation(_ context.Context, _ string) (any, error) {
	if s.correlateErr != nil {
		return nil, s.correlateErr
	}
	return map[string]any{"spans": []any{}}, nil
}

func (s *stubPlaygroundController) Replay(_ context.Context, _, _, _ string) (*PlaygroundRunDTO, error) {
	if s.replayErr != nil {
		return nil, s.replayErr
	}
	return &PlaygroundRunDTO{ID: "run_1", Status: "ok"}, nil
}

// in-memory playground store for handler tests.
type memPlaygroundStore struct {
	cases map[string]*ifaces.PlaygroundCaseRecord
	runs  map[string]*ifaces.PlaygroundRunRecord
}

func newMemPlaygroundStore() *memPlaygroundStore {
	return &memPlaygroundStore{
		cases: make(map[string]*ifaces.PlaygroundCaseRecord),
		runs:  make(map[string]*ifaces.PlaygroundRunRecord),
	}
}

func (m *memPlaygroundStore) UpsertCase(_ context.Context, c *ifaces.PlaygroundCaseRecord) error {
	if c == nil || c.TenantID == "" || c.CaseID == "" {
		return errors.New("invalid case")
	}
	m.cases[c.TenantID+"|"+c.CaseID] = c
	return nil
}
func (m *memPlaygroundStore) GetCase(_ context.Context, t, id string) (*ifaces.PlaygroundCaseRecord, error) {
	c, ok := m.cases[t+"|"+id]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	return c, nil
}
func (m *memPlaygroundStore) ListCases(_ context.Context, t string, _ ifaces.PlaygroundCasesQuery) ([]*ifaces.PlaygroundCaseRecord, string, error) {
	out := []*ifaces.PlaygroundCaseRecord{}
	prefix := t + "|"
	for k, c := range m.cases {
		if strings.HasPrefix(k, prefix) {
			out = append(out, c)
		}
	}
	return out, "", nil
}
func (m *memPlaygroundStore) DeleteCase(_ context.Context, t, id string) error {
	k := t + "|" + id
	if _, ok := m.cases[k]; !ok {
		return ifaces.ErrNotFound
	}
	delete(m.cases, k)
	return nil
}
func (m *memPlaygroundStore) InsertRun(_ context.Context, r *ifaces.PlaygroundRunRecord) error {
	m.runs[r.TenantID+"|"+r.RunID] = r
	return nil
}
func (m *memPlaygroundStore) UpdateRun(_ context.Context, r *ifaces.PlaygroundRunRecord) error {
	if _, ok := m.runs[r.TenantID+"|"+r.RunID]; !ok {
		return ifaces.ErrNotFound
	}
	m.runs[r.TenantID+"|"+r.RunID] = r
	return nil
}
func (m *memPlaygroundStore) GetRun(_ context.Context, t, id string) (*ifaces.PlaygroundRunRecord, error) {
	r, ok := m.runs[t+"|"+id]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	return r, nil
}
func (m *memPlaygroundStore) ListRuns(_ context.Context, t string, _ ifaces.PlaygroundRunsQuery) ([]*ifaces.PlaygroundRunRecord, string, error) {
	out := []*ifaces.PlaygroundRunRecord{}
	prefix := t + "|"
	for k, r := range m.runs {
		if strings.HasPrefix(k, prefix) {
			out = append(out, r)
		}
	}
	return out, "", nil
}

func playgroundDeps(t *testing.T) (Deps, *stubPlaygroundController, *memPlaygroundStore) {
	t.Helper()
	d, _, _, _, _, _, _, _, _, _ := testDeps(t)
	ctl := &stubPlaygroundController{}
	store := newMemPlaygroundStore()
	d.Playground = ctl
	d.PlaygroundStore = store
	return d, ctl, store
}

func playgroundReq(method, path string, body any) *http.Request {
	r := newReq(method, path, body)
	id := tenant.Identity{TenantID: "tenant-a", UserID: "alice", Scopes: []string{"admin"}}
	return r.WithContext(tenant.With(r.Context(), id))
}

func TestPlaygroundHandlers_StartSession(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	r := playgroundReq("POST", "/api/playground/sessions", map[string]string{})
	w := runHandler(startPlaygroundSessionHandler(d), r)
	statusOK(t, w, 201)
}

func TestPlaygroundHandlers_StartSession_NilController(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	d.Playground = nil
	r := playgroundReq("POST", "/api/playground/sessions", map[string]string{})
	w := runHandler(startPlaygroundSessionHandler(d), r)
	statusOK(t, w, 503)
}

func TestPlaygroundHandlers_EndSession(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	r := playgroundReq("DELETE", "/api/playground/sessions/x", nil)
	r = withChiURLParam(r, "sid", "x")
	w := runHandler(endPlaygroundSessionHandler(d), r)
	statusOK(t, w, 204)
}

func TestPlaygroundHandlers_Catalog(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	r := playgroundReq("GET", "/api/playground/sessions/x/catalog", nil)
	r = withChiURLParam(r, "sid", "x")
	w := runHandler(catalogPlaygroundHandler(d), r)
	statusOK(t, w, 200)
}

func TestPlaygroundHandlers_IssueCall(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	r := playgroundReq("POST", "/api/playground/sessions/x/calls",
		PlaygroundCallRequest{Kind: "tool_call", Target: "x.y"})
	r = withChiURLParam(r, "sid", "x")
	w := runHandler(issueCallHandler(d), r)
	statusOK(t, w, 202)
}

func TestPlaygroundHandlers_IssueCall_BadJSON(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	r := newReq("POST", "/api/playground/sessions/x/calls", nil)
	r.Body = http.NoBody
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(tenant.With(r.Context(),
		tenant.Identity{TenantID: "tenant-a", UserID: "alice", Scopes: []string{"admin"}}))
	r = withChiURLParam(r, "sid", "x")
	// Manually feed invalid JSON.
	r.Body = http.NoBody
	r.Body = http.NoBody
	// We can't easily send malformed JSON via newReq; this test just asserts
	// the handler doesn't panic on missing body.
	w := httptest.NewRecorder()
	issueCallHandler(d).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusAccepted {
		t.Fatalf("unexpected status %d", w.Code)
	}
}

func TestPlaygroundHandlers_StreamCall(t *testing.T) {
	d, ctl, _ := playgroundDeps(t)
	ctl.streamFrames = []PlaygroundStreamFrame{{Type: "chunk", Data: json.RawMessage(`"hi"`)}}
	r := playgroundReq("GET", "/api/playground/sessions/x/calls/y/stream", nil)
	r = withChiURLParam(r, "sid", "x")
	r = withChiURLParam(r, "cid", "y")
	w := runHandler(streamCallHandler(d), r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "event: chunk") {
		t.Fatalf("expected 'event: chunk' in body: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "event: end") {
		t.Fatalf("expected 'event: end' in body: %s", w.Body.String())
	}
}

func TestPlaygroundHandlers_Correlation(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	r := playgroundReq("GET", "/api/playground/sessions/x/correlation?since=invalid", nil)
	r = withChiURLParam(r, "sid", "x")
	w := runHandler(playgroundCorrelationHandler(d), r)
	statusOK(t, w, 200)
}

func TestPlaygroundHandlers_RunCorrelation(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	r := playgroundReq("GET", "/api/playground/runs/r1/correlation", nil)
	r = withChiURLParam(r, "run_id", "r1")
	w := runHandler(runCorrelationHandler(d), r)
	statusOK(t, w, 200)
}

func TestPlaygroundHandlers_CRUD(t *testing.T) {
	d, _, _ := playgroundDeps(t)

	// Create.
	body := PlaygroundCaseDTO{
		Name:    "smoke",
		Kind:    "tool_call",
		Target:  "x.y",
		Payload: json.RawMessage(`{}`),
		Tags:    []string{"smoke"},
	}
	r := playgroundReq("POST", "/api/playground/cases", body)
	w := runHandler(createPlaygroundCaseHandler(d), r)
	statusOK(t, w, 201)
	var created PlaygroundCaseDTO
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// Get.
	r = playgroundReq("GET", "/api/playground/cases/"+created.ID, nil)
	r = withChiURLParam(r, "id", created.ID)
	w = runHandler(getPlaygroundCaseHandler(d), r)
	statusOK(t, w, 200)

	// Get unknown -> 404.
	r = playgroundReq("GET", "/api/playground/cases/nope", nil)
	r = withChiURLParam(r, "id", "nope")
	w = runHandler(getPlaygroundCaseHandler(d), r)
	statusOK(t, w, 404)

	// List.
	r = playgroundReq("GET", "/api/playground/cases", nil)
	w = runHandler(listPlaygroundCasesHandler(d), r)
	statusOK(t, w, 200)

	// Update.
	body.Name = "smoke-renamed"
	r = playgroundReq("PUT", "/api/playground/cases/"+created.ID, body)
	r = withChiURLParam(r, "id", created.ID)
	w = runHandler(updatePlaygroundCaseHandler(d), r)
	statusOK(t, w, 200)

	// Replay.
	r = playgroundReq("POST", "/api/playground/cases/"+created.ID+"/replay", nil)
	r = withChiURLParam(r, "id", created.ID)
	w = runHandler(replayPlaygroundCaseHandler(d), r)
	statusOK(t, w, 202)

	// Delete.
	r = playgroundReq("DELETE", "/api/playground/cases/"+created.ID, nil)
	r = withChiURLParam(r, "id", created.ID)
	w = runHandler(deletePlaygroundCaseHandler(d), r)
	statusOK(t, w, 204)

	// Delete unknown -> 404.
	r = playgroundReq("DELETE", "/api/playground/cases/zzz", nil)
	r = withChiURLParam(r, "id", "zzz")
	w = runHandler(deletePlaygroundCaseHandler(d), r)
	statusOK(t, w, 404)
}

func TestPlaygroundHandlers_RunDetail_NotFound(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	r := playgroundReq("GET", "/api/playground/runs/x", nil)
	r = withChiURLParam(r, "run_id", "x")
	w := runHandler(runDetailHandler(d), r)
	statusOK(t, w, 404)
}

func TestPlaygroundHandlers_CaseRuns(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	r := playgroundReq("GET", "/api/playground/cases/x/runs", nil)
	r = withChiURLParam(r, "id", "x")
	w := runHandler(caseRunsHandler(d), r)
	statusOK(t, w, 200)
}

func TestPlaygroundHandlers_StoreUnavailable(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	d.PlaygroundStore = nil
	r := playgroundReq("GET", "/api/playground/cases", nil)
	w := runHandler(listPlaygroundCasesHandler(d), r)
	statusOK(t, w, 503)
}

func TestIntersectScopes(t *testing.T) {
	out := intersectScopes([]string{"a", "b"}, []string{"b", "c"})
	if len(out) != 1 || out[0] != "b" {
		t.Fatalf("expected [b], got %v", out)
	}
	// Empty operator list returns input unchanged.
	out = intersectScopes([]string{"x"}, nil)
	if len(out) != 1 {
		t.Fatalf("expected [x], got %v", out)
	}
}

// Test more error branches.

func TestPlaygroundHandlers_StartSessionCrossTenant_Forbidden(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	r := newReq("POST", "/api/playground/sessions", PlaygroundStartSessionRequest{TenantID: "other"})
	r = r.WithContext(tenant.With(r.Context(),
		tenant.Identity{TenantID: "tenant-a", UserID: "alice", Scopes: []string{"servers:read"}}))
	w := runHandler(startPlaygroundSessionHandler(d), r)
	statusOK(t, w, 403)
}

func TestPlaygroundHandlers_StartSession_BadInput(t *testing.T) {
	d, ctl, _ := playgroundDeps(t)
	ctl.startErr = errors.New("boom")
	r := playgroundReq("POST", "/api/playground/sessions", map[string]string{})
	w := runHandler(startPlaygroundSessionHandler(d), r)
	statusOK(t, w, 400)
}

func TestPlaygroundHandlers_EndSession_NotFound(t *testing.T) {
	d, ctl, _ := playgroundDeps(t)
	ctl.endErr = errors.New("not found")
	r := playgroundReq("DELETE", "/api/playground/sessions/x", nil)
	r = withChiURLParam(r, "sid", "x")
	w := runHandler(endPlaygroundSessionHandler(d), r)
	statusOK(t, w, 404)
}

func TestPlaygroundHandlers_EndSession_NilController(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	d.Playground = nil
	r := playgroundReq("DELETE", "/api/playground/sessions/x", nil)
	r = withChiURLParam(r, "sid", "x")
	w := runHandler(endPlaygroundSessionHandler(d), r)
	statusOK(t, w, 503)
}

func TestPlaygroundHandlers_CatalogError(t *testing.T) {
	d, ctl, _ := playgroundDeps(t)
	ctl.catalogErr = errors.New("session gone")
	r := playgroundReq("GET", "/api/playground/sessions/x/catalog", nil)
	r = withChiURLParam(r, "sid", "x")
	w := runHandler(catalogPlaygroundHandler(d), r)
	statusOK(t, w, 404)
}

func TestPlaygroundHandlers_CatalogNilController(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	d.Playground = nil
	r := playgroundReq("GET", "/api/playground/sessions/x/catalog", nil)
	r = withChiURLParam(r, "sid", "x")
	w := runHandler(catalogPlaygroundHandler(d), r)
	statusOK(t, w, 503)
}

func TestPlaygroundHandlers_IssueCallNilController(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	d.Playground = nil
	r := playgroundReq("POST", "/api/playground/sessions/x/calls",
		PlaygroundCallRequest{Kind: "tool_call", Target: "x.y"})
	r = withChiURLParam(r, "sid", "x")
	w := runHandler(issueCallHandler(d), r)
	statusOK(t, w, 503)
}

func TestPlaygroundHandlers_IssueCallControllerError(t *testing.T) {
	d, ctl, _ := playgroundDeps(t)
	ctl.callErr = errors.New("call refused")
	r := playgroundReq("POST", "/api/playground/sessions/x/calls",
		PlaygroundCallRequest{Kind: "tool_call", Target: "x.y"})
	r = withChiURLParam(r, "sid", "x")
	w := runHandler(issueCallHandler(d), r)
	statusOK(t, w, 400)
}

func TestPlaygroundHandlers_StreamCallNilController(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	d.Playground = nil
	r := playgroundReq("GET", "/api/playground/sessions/x/calls/y/stream", nil)
	r = withChiURLParam(r, "sid", "x")
	r = withChiURLParam(r, "cid", "y")
	w := runHandler(streamCallHandler(d), r)
	statusOK(t, w, 503)
}

func TestPlaygroundHandlers_StreamCallError(t *testing.T) {
	d, ctl, _ := playgroundDeps(t)
	ctl.streamErr = errors.New("stream failed")
	r := playgroundReq("GET", "/api/playground/sessions/x/calls/y/stream", nil)
	r = withChiURLParam(r, "sid", "x")
	r = withChiURLParam(r, "cid", "y")
	w := runHandler(streamCallHandler(d), r)
	statusOK(t, w, 404)
}

func TestPlaygroundHandlers_CorrelationNilController(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	d.Playground = nil
	r := playgroundReq("GET", "/api/playground/sessions/x/correlation", nil)
	r = withChiURLParam(r, "sid", "x")
	w := runHandler(playgroundCorrelationHandler(d), r)
	statusOK(t, w, 503)
}

func TestPlaygroundHandlers_RunCorrelationNilController(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	d.Playground = nil
	r := playgroundReq("GET", "/api/playground/runs/r1/correlation", nil)
	r = withChiURLParam(r, "run_id", "r1")
	w := runHandler(runCorrelationHandler(d), r)
	statusOK(t, w, 503)
}

func TestPlaygroundHandlers_ReplayError(t *testing.T) {
	d, ctl, _ := playgroundDeps(t)
	ctl.replayErr = errors.New("case missing")
	r := playgroundReq("POST", "/api/playground/cases/missing/replay", nil)
	r = withChiURLParam(r, "id", "missing")
	w := runHandler(replayPlaygroundCaseHandler(d), r)
	statusOK(t, w, 400)
}

func TestPlaygroundHandlers_ReplayNilController(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	d.Playground = nil
	r := playgroundReq("POST", "/api/playground/cases/x/replay", nil)
	r = withChiURLParam(r, "id", "x")
	w := runHandler(replayPlaygroundCaseHandler(d), r)
	statusOK(t, w, 503)
}

func TestPlaygroundHandlers_CRUDStoreUnavailable(t *testing.T) {
	d, _, _ := playgroundDeps(t)
	d.PlaygroundStore = nil
	cases := []http.HandlerFunc{
		listPlaygroundCasesHandler(d),
		createPlaygroundCaseHandler(d),
		getPlaygroundCaseHandler(d),
		updatePlaygroundCaseHandler(d),
		deletePlaygroundCaseHandler(d),
		caseRunsHandler(d),
		runDetailHandler(d),
	}
	for _, h := range cases {
		r := playgroundReq("GET", "/x", nil)
		w := runHandler(h, r)
		if w.Code != 503 {
			t.Fatalf("expected 503, got %d", w.Code)
		}
	}
}

func TestCaseAndRunDTO(t *testing.T) {
	c := caseToDTO(nil)
	if c.ID != "" || c.Name != "" || c.Kind != "" {
		t.Fatalf("nil case should yield zero DTO, got %+v", c)
	}
	r := runToDTO(nil)
	if r.ID != "" || r.Status != "" {
		t.Fatalf("nil run should yield zero DTO, got %+v", r)
	}
}
