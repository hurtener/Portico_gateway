package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// PlaygroundController is the api-package surface backed by the runtime
// playground service in cmd/portico/phase10_wiring.go. The interface
// keeps internal/server/api free of the heavyweight dependency chain
// (snapshots, audit emitter, dispatcher) — phase10_wiring.go injects an
// adapter that closes over the real types.
type PlaygroundController interface {
	// Sessions.
	StartSession(ctx context.Context, req PlaygroundStartSessionRequest) (*PlaygroundSessionDTO, error)
	EndSession(ctx context.Context, sid string) error
	GetSession(sid string) *PlaygroundSessionDTO

	// Catalog (snapshot-bound).
	Catalog(ctx context.Context, sid string) (*PlaygroundCatalogDTO, error)

	// Calls (the dispatcher seam).
	IssueCall(ctx context.Context, sid string, req PlaygroundCallRequest) (*PlaygroundCallEnvelope, error)
	StreamCall(ctx context.Context, sid, cid string) (<-chan PlaygroundStreamFrame, error)

	// Correlation.
	Correlation(ctx context.Context, sid string, since time.Time) (any, error)
	RunCorrelation(ctx context.Context, runID string) (any, error)

	// Replay.
	Replay(ctx context.Context, tenantID, actorID, caseID string) (*PlaygroundRunDTO, error)
}

// PlaygroundStartSessionRequest is the JSON body for POST /api/playground/sessions.
type PlaygroundStartSessionRequest struct {
	TenantID        string   `json:"tenant_id,omitempty"`
	SnapshotID      string   `json:"snapshot_id,omitempty"`
	RuntimeOverride string   `json:"runtime_override,omitempty"`
	Scopes          []string `json:"scopes,omitempty"`
}

// PlaygroundSessionDTO is the wire shape for a session.
type PlaygroundSessionDTO struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	ActorID    string    `json:"actor_id,omitempty"`
	SnapshotID string    `json:"snapshot_id,omitempty"`
	Token      string    `json:"token"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// PlaygroundCatalogDTO is a thin wrapper around the snapshot the
// frontend renders. Kept narrow so phase10_wiring.go can populate it
// without leaking a hard dependency on the snapshots package types
// through the api seam.
type PlaygroundCatalogDTO struct {
	SnapshotID string         `json:"snapshot_id"`
	Catalog    map[string]any `json:"catalog"`
}

// PlaygroundCallRequest is the body for POST /sessions/{sid}/calls.
type PlaygroundCallRequest struct {
	Kind   string          `json:"kind"`   // tool_call | resource_read | prompt_get
	Target string          `json:"target"` // <server>.<tool> | uri | prompt name
	Args   json.RawMessage `json:"arguments,omitempty"`
}

// PlaygroundCallEnvelope is the shape returned synchronously when a call
// is enqueued; the operator opens an SSE on /stream to receive frames.
type PlaygroundCallEnvelope struct {
	CallID    string `json:"call_id"`
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

// PlaygroundStreamFrame is one SSE frame emitted by StreamCall.
type PlaygroundStreamFrame struct {
	Type string          `json:"type"` // chunk | error | end | comment
	Data json.RawMessage `json:"data,omitempty"`
}

// PlaygroundRunDTO is the wire shape of a Run row.
type PlaygroundRunDTO struct {
	ID            string    `json:"id"`
	CaseID        string    `json:"case_id,omitempty"`
	SessionID     string    `json:"session_id"`
	SnapshotID    string    `json:"snapshot_id"`
	Status        string    `json:"status"`
	DriftDetected bool      `json:"drift_detected"`
	Summary       string    `json:"summary,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at,omitempty"`
}

// PlaygroundCaseDTO is the wire shape for saved cases.
type PlaygroundCaseDTO struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Kind        string          `json:"kind"`
	Target      string          `json:"target"`
	Payload     json.RawMessage `json:"payload"`
	SnapshotID  string          `json:"snapshot_id,omitempty"`
	Tags        []string        `json:"tags"`
	CreatedAt   time.Time       `json:"created_at"`
	CreatedBy   string          `json:"created_by,omitempty"`
}

// startPlaygroundSessionHandler POST /api/playground/sessions.
func startPlaygroundSessionHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Playground == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		var body PlaygroundStartSessionRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.TenantID == "" {
			body.TenantID = id.TenantID
		}
		// Operators can only target their own tenant unless they hold admin.
		if body.TenantID != id.TenantID && !id.HasScope("admin") {
			writeJSONError(w, http.StatusForbidden, "cross_tenant_forbidden", "admin scope required for cross-tenant sessions", nil)
			return
		}
		// Cap the requested scopes to what the operator already holds —
		// the playground can never escalate.
		body.Scopes = intersectScopes(body.Scopes, id.Scopes)
		sess, err := d.Playground.StartSession(r.Context(), body)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "start_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusCreated, sess)
	}
}

// endPlaygroundSessionHandler DELETE /api/playground/sessions/{sid}.
func endPlaygroundSessionHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Playground == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground not configured", nil)
			return
		}
		sid := chi.URLParam(r, "sid")
		if err := d.Playground.EndSession(r.Context(), sid); err != nil {
			writeJSONError(w, http.StatusNotFound, "not_found", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// catalogPlaygroundHandler GET /api/playground/sessions/{sid}/catalog.
func catalogPlaygroundHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Playground == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground not configured", nil)
			return
		}
		sid := chi.URLParam(r, "sid")
		cat, err := d.Playground.Catalog(r.Context(), sid)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "catalog_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, cat)
	}
}

// issueCallHandler POST /api/playground/sessions/{sid}/calls.
func issueCallHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Playground == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground not configured", nil)
			return
		}
		sid := chi.URLParam(r, "sid")
		var body PlaygroundCallRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		env, err := d.Playground.IssueCall(r.Context(), sid, body)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "call_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusAccepted, env)
	}
}

// streamCallHandler GET /api/playground/sessions/{sid}/calls/{cid}/stream.
// SSE stream of JSON-RPC chunks for the call.
func streamCallHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Playground == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground not configured", nil)
			return
		}
		sid := chi.URLParam(r, "sid")
		cid := chi.URLParam(r, "cid")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "no_flusher", "streaming not supported", nil)
			return
		}
		ch, err := d.Playground.StreamCall(r.Context(), sid, cid)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "stream_failed", err.Error(), nil)
			return
		}
		// Initial connect comment.
		_, _ = w.Write([]byte(": connected\n\n"))
		flusher.Flush()
		hb := time.NewTicker(15 * time.Second)
		defer hb.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-hb.C:
				_, _ = w.Write([]byte(": keep-alive\n\n"))
				flusher.Flush()
			case fr, ok := <-ch:
				if !ok {
					return
				}
				body, _ := json.Marshal(fr)
				_, _ = w.Write([]byte("event: " + fr.Type + "\ndata: "))
				_, _ = w.Write(body)
				_, _ = w.Write([]byte("\n\n"))
				flusher.Flush()
				if fr.Type == "end" || fr.Type == "error" {
					return
				}
			}
		}
	}
}

// listPlaygroundCasesHandler GET /api/playground/cases.
func listPlaygroundCasesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := backendPlaygroundStore(d)
		if store == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground store not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		q := ifaces.PlaygroundCasesQuery{
			Limit:  100,
			Cursor: r.URL.Query().Get("cursor"),
			Tag:    r.URL.Query().Get("tag"),
			Kind:   r.URL.Query().Get("kind"),
		}
		recs, next, err := store.ListCases(r.Context(), id.TenantID, q)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]PlaygroundCaseDTO, 0, len(recs))
		for _, rec := range recs {
			out = append(out, caseToDTO(rec))
		}
		writeJSON(w, http.StatusOK, map[string]any{"cases": out, "next_cursor": next})
	}
}

// createPlaygroundCaseHandler POST /api/playground/cases.
func createPlaygroundCaseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := backendPlaygroundStore(d)
		if store == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground store not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		var body PlaygroundCaseDTO
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if body.ID == "" {
			body.ID = "case_" + ulid.MustNew(ulid.Timestamp(time.Now().UTC()), ulid.DefaultEntropy()).String()
		}
		rec := &ifaces.PlaygroundCaseRecord{
			TenantID:    id.TenantID,
			CaseID:      body.ID,
			Name:        body.Name,
			Description: body.Description,
			Kind:        body.Kind,
			Target:      body.Target,
			Payload:     body.Payload,
			SnapshotID:  body.SnapshotID,
			Tags:        body.Tags,
			CreatedAt:   time.Now().UTC(),
			CreatedBy:   id.UserID,
		}
		if err := store.UpsertCase(r.Context(), rec); err != nil {
			writeJSONError(w, http.StatusBadRequest, "save_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusCreated, caseToDTO(rec))
	}
}

// getPlaygroundCaseHandler GET /api/playground/cases/{id}.
func getPlaygroundCaseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := backendPlaygroundStore(d)
		if store == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground store not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		caseID := chi.URLParam(r, "id")
		rec, err := store.GetCase(r.Context(), id.TenantID, caseID)
		if err != nil {
			if errors.Is(err, ifaces.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "case not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, caseToDTO(rec))
	}
}

// updatePlaygroundCaseHandler PUT /api/playground/cases/{id}.
func updatePlaygroundCaseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := backendPlaygroundStore(d)
		if store == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground store not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		caseID := chi.URLParam(r, "id")
		var body PlaygroundCaseDTO
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		body.ID = caseID
		rec := &ifaces.PlaygroundCaseRecord{
			TenantID:    id.TenantID,
			CaseID:      caseID,
			Name:        body.Name,
			Description: body.Description,
			Kind:        body.Kind,
			Target:      body.Target,
			Payload:     body.Payload,
			SnapshotID:  body.SnapshotID,
			Tags:        body.Tags,
			CreatedAt:   time.Now().UTC(),
			CreatedBy:   id.UserID,
		}
		if err := store.UpsertCase(r.Context(), rec); err != nil {
			writeJSONError(w, http.StatusBadRequest, "save_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, caseToDTO(rec))
	}
}

// deletePlaygroundCaseHandler DELETE /api/playground/cases/{id}.
func deletePlaygroundCaseHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := backendPlaygroundStore(d)
		if store == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground store not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		caseID := chi.URLParam(r, "id")
		if err := store.DeleteCase(r.Context(), id.TenantID, caseID); err != nil {
			if errors.Is(err, ifaces.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "case not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// caseRunsHandler GET /api/playground/cases/{id}/runs.
func caseRunsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := backendPlaygroundStore(d)
		if store == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground store not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		caseID := chi.URLParam(r, "id")
		runs, next, err := store.ListRuns(r.Context(), id.TenantID, ifaces.PlaygroundRunsQuery{CaseID: caseID, Limit: 100})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]PlaygroundRunDTO, 0, len(runs))
		for _, rec := range runs {
			out = append(out, runToDTO(rec))
		}
		writeJSON(w, http.StatusOK, map[string]any{"runs": out, "next_cursor": next})
	}
}

// runDetailHandler GET /api/playground/runs/{run_id}.
func runDetailHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store := backendPlaygroundStore(d)
		if store == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "playground_unavailable", "playground store not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		runID := chi.URLParam(r, "run_id")
		rec, err := store.GetRun(r.Context(), id.TenantID, runID)
		if err != nil {
			if errors.Is(err, ifaces.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "run not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, runToDTO(rec))
	}
}

// helpers ---------------------------------------------------------------

// backendPlaygroundStore returns the playground store from Deps.PlaygroundStore.
func backendPlaygroundStore(d Deps) ifaces.PlaygroundStore {
	return d.PlaygroundStore
}

func caseToDTO(r *ifaces.PlaygroundCaseRecord) PlaygroundCaseDTO {
	if r == nil {
		return PlaygroundCaseDTO{}
	}
	return PlaygroundCaseDTO{
		ID:          r.CaseID,
		Name:        r.Name,
		Description: r.Description,
		Kind:        r.Kind,
		Target:      r.Target,
		Payload:     r.Payload,
		SnapshotID:  r.SnapshotID,
		Tags:        r.Tags,
		CreatedAt:   r.CreatedAt,
		CreatedBy:   r.CreatedBy,
	}
}

func runToDTO(r *ifaces.PlaygroundRunRecord) PlaygroundRunDTO {
	if r == nil {
		return PlaygroundRunDTO{}
	}
	return PlaygroundRunDTO{
		ID:            r.RunID,
		CaseID:        r.CaseID,
		SessionID:     r.SessionID,
		SnapshotID:    r.SnapshotID,
		Status:        r.Status,
		DriftDetected: r.DriftDetected,
		Summary:       r.Summary,
		StartedAt:     r.StartedAt,
		EndedAt:       r.EndedAt,
	}
}

// intersectScopes returns the elements present in both a and b. Empty
// `b` returns `a` unchanged so callers without a known operator scope
// list don't accidentally strip everything.
func intersectScopes(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	have := make(map[string]struct{}, len(b))
	for _, s := range b {
		have[s] = struct{}{}
	}
	out := make([]string, 0, len(a))
	for _, s := range a {
		if _, ok := have[s]; ok {
			out = append(out, s)
		}
	}
	return out
}
