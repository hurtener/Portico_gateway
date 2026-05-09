// handlers_session_replay.go — Phase 11 deliverable #8.
//
// Replay-from-inspector POSTs to:
//
//   POST /api/sessions/{sid}/calls/{cid}/replay
//
// The client (the inspector) supplies the tool + argument payload it
// reconstructed from the audit lane. The handler turns that into a
// transient Phase 10 Case and immediately calls Playground.Replay,
// returning the Run row so the inspector can deep-link to it.
//
// The `cid` path component is the original span/call identifier from
// the inspector; we use it to label the saved case so it's
// recognisable in the playground listing.

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// replayCallRequest is the wire body for the replay endpoint.
type replayCallRequest struct {
	// Kind: tool_call | resource_read | prompt_get. Same vocabulary
	// as the playground IssueCall surface.
	Kind string `json:"kind"`
	// Target is the namespaced tool/resource/prompt name.
	Target string `json:"target"`
	// Payload is the raw call payload — frontend forwards the audit
	// row's payload after stripping system fields.
	Payload json.RawMessage `json:"payload"`
	// SnapshotID pins the replay to the same catalog the original
	// call ran against. Empty falls back to the playground default.
	SnapshotID string `json:"snapshot_id,omitempty"`
	// Name overrides the auto-generated case name. Empty: use
	// "Replay <sid>/<cid>".
	Name string `json:"name,omitempty"`
}

// replayCallHandler implements POST /api/sessions/{sid}/calls/{cid}/replay.
//
// Lifecycle:
//  1. Materialise a Phase 10 Case under id "replay-<sid>-<cid>".
//  2. Invoke Playground.Replay against it.
//  3. Return the Run + the case id so the inspector can navigate to
//     /playground/cases/<case_id> or /playground/runs/<run_id>.
//
// Imported sessions cannot be replayed (the source instance is the
// only place it could meaningfully run); we return 409 in that case.
func replayCallHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Playground == nil || d.PlaygroundStore == nil {
			writeJSONError(w, http.StatusServiceUnavailable,
				"playground_unavailable", "playground not configured", nil)
			return
		}

		id := tenant.MustFrom(r.Context())
		sid := chi.URLParam(r, "sid")
		cid := chi.URLParam(r, "cid")
		if sid == "" || cid == "" {
			writeJSONError(w, http.StatusBadRequest,
				"missing_id", "session id and call id required", nil)
			return
		}
		if isImportedSession(sid) {
			writeJSONError(w, http.StatusConflict,
				"replay_imported_disallowed",
				"imported sessions are read-only — replay is not available", nil)
			return
		}

		var body replayCallRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest,
				"invalid_body", err.Error(), nil)
			return
		}
		if body.Kind == "" || body.Target == "" {
			writeJSONError(w, http.StatusBadRequest,
				"missing_field", "kind and target required", nil)
			return
		}

		caseID := fmt.Sprintf("replay-%s-%s", sid, cid)
		name := body.Name
		if name == "" {
			name = fmt.Sprintf("Replay %s/%s", sid, cid)
		}
		payload := body.Payload
		if len(payload) == 0 {
			payload = json.RawMessage(`{}`)
		}

		// Save (or upsert) the case so the operator can re-run it
		// from /playground later. UpsertCase is idempotent on (tenant,
		// case_id) so re-clicking Replay is safe.
		rec := &ifaces.PlaygroundCaseRecord{
			TenantID:    id.TenantID,
			CaseID:      caseID,
			Name:        name,
			Description: fmt.Sprintf("Replay of session %s call %s", sid, cid),
			Kind:        body.Kind,
			Target:      body.Target,
			Payload:     payload,
			SnapshotID:  body.SnapshotID,
			Tags:        []string{"replayed", "from-session:" + sid},
			CreatedAt:   time.Now().UTC(),
			CreatedBy:   id.UserID,
		}
		if err := d.PlaygroundStore.UpsertCase(r.Context(), rec); err != nil {
			writeJSONError(w, http.StatusInternalServerError,
				"case_persist_failed", err.Error(), nil)
			return
		}

		run, err := d.Playground.Replay(r.Context(), id.TenantID, id.UserID, caseID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest,
				"replay_failed", err.Error(), nil)
			return
		}

		writeJSON(w, http.StatusAccepted, map[string]any{
			"case_id": caseID,
			"run":     run,
			// Echo the source session/call so the inspector can stamp
			// "replay_of" links without a second round-trip.
			"replay_of_session_id": sid,
			"replay_of_call_id":    cid,
		})
	}
}

// isImportedSession returns true for synthetic ids issued by the
// bundle importer.
func isImportedSession(sid string) bool {
	const prefix = "imported:"
	if len(sid) < len(prefix) {
		return false
	}
	return sid[:len(prefix)] == prefix
}

// guard against unused imports for future-only error wrapping.
var _ = errors.New
