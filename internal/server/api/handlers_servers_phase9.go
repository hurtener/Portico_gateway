package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// Phase 9 server endpoints: /api/servers/{id}/restart, /logs, /health,
// /activity, plus PATCH for partial updates. The Phase 2 endpoints under
// /v1/servers are kept for back-compat.

// restartServerHandler POST /api/servers/{id}/restart — body: {reason}.
func restartServerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Registry == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "registry_unavailable", "registry not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		serverID := chi.URLParam(r, "id")
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // body optional
		snap, err := d.Registry.Restart(r.Context(), id.TenantID, serverID, body.Reason)
		if err != nil {
			if IsErrNotFound(err) {
				writeJSONError(w, http.StatusNotFound, "not_found", "server not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "restart_failed", err.Error(), nil)
			return
		}
		if d.ServerRuntime != nil {
			_ = d.ServerRuntime.RecordRestart(r.Context(), id.TenantID, serverID, body.Reason, time.Now().UTC())
		}
		emitEntityEvent(d, r, audit.EventServerRestarted, "server", serverID, "server.restarted",
			map[string]any{"server_id": serverID, "reason": body.Reason})
		writeJSON(w, http.StatusAccepted, snapshotJSON(snap))
	}
}

// logsServerHandler GET /api/servers/{id}/logs — SSE stream of stdout +
// stderr lines from the supervisor's per-process ring buffer. Phase 10
// fills in the live tail; Phase 9 shipped the placeholder route.
//
// Query params: ?since=<RFC3339Nano> filters historical lines.
func logsServerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Registry == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "registry_unavailable", "registry not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		serverID := chi.URLParam(r, "id")
		// Confirm the server exists.
		if _, err := d.Registry.Get(r.Context(), id.TenantID, serverID); err != nil {
			if IsErrNotFound(err) {
				writeJSONError(w, http.StatusNotFound, "not_found", "server not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		var since time.Time
		if s := r.URL.Query().Get("since"); s != "" {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				since = t
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "no_flusher", "streaming not supported", nil)
			return
		}
		// Initial comment line so the EventSource immediately considers
		// itself connected (browsers wait for the first byte before firing
		// `onopen`).
		fmt.Fprint(w, ": connected\n\n")
		flusher.Flush()

		ch, err := d.Registry.Logs(r.Context(), id.TenantID, serverID, since)
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: %q\n\n", err.Error())
			flusher.Flush()
			return
		}
		// 15s heartbeat so reverse proxies don't kill an idle SSE.
		hb := time.NewTicker(15 * time.Second)
		defer hb.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-hb.C:
				fmt.Fprint(w, ": keep-alive\n\n")
				flusher.Flush()
			case line, ok := <-ch:
				if !ok {
					// Stream ended; close the SSE so the client knows
					// (EventSource will auto-reconnect, which is fine —
					// the Console only opens this stream when a server
					// page is mounted).
					return
				}
				payload, _ := jsonEncode(map[string]any{
					"at":     line.At.Format(time.RFC3339Nano),
					"stream": line.Stream,
					"text":   line.Text,
				})
				fmt.Fprintf(w, "event: log\ndata: %s\n\n", string(payload))
				flusher.Flush()
			}
		}
	}
}

// healthServerHandler GET /api/servers/{id}/health — returns the
// supervisor's last-known status fields.
func healthServerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Registry == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "registry_unavailable", "registry not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		serverID := chi.URLParam(r, "id")
		snap, err := d.Registry.Get(r.Context(), id.TenantID, serverID)
		if err != nil {
			if IsErrNotFound(err) {
				writeJSONError(w, http.StatusNotFound, "not_found", "server not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"server_id":     snap.Spec.ID,
			"status":        snap.Record.Status,
			"status_detail": snap.Record.StatusDetail,
			"enabled":       snap.Record.Enabled,
			"last_error":    snap.Record.LastError,
			"updated_at":    snap.Record.UpdatedAt,
		})
	}
}

// patchServerHandler PATCH /api/servers/{id} — body may contain {enabled?,
// env_overrides?}. Storage: env overrides land in tenant_servers_runtime,
// enabled toggles via Registry.SetEnabled.
func patchServerHandler(d Deps) http.HandlerFunc {
	type body struct {
		Enabled      *bool             `json:"enabled,omitempty"`
		EnvOverrides map[string]string `json:"env_overrides,omitempty"`
		Reason       string            `json:"reason,omitempty"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Registry == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "registry_unavailable", "registry not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		serverID := chi.URLParam(r, "id")
		var b body
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		snap, err := d.Registry.Get(r.Context(), id.TenantID, serverID)
		if err != nil {
			if IsErrNotFound(err) {
				writeJSONError(w, http.StatusNotFound, "not_found", "server not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		// Toggle enabled via the existing path.
		if b.Enabled != nil {
			if _, err := d.Registry.SetEnabled(r.Context(), id.TenantID, serverID, *b.Enabled); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "set_enabled_failed", err.Error(), nil)
				return
			}
		}
		// Persist env overrides.
		if b.EnvOverrides != nil && d.ServerRuntime != nil {
			envBytes, err := json.Marshal(b.EnvOverrides)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_env", err.Error(), nil)
				return
			}
			rec, _ := d.ServerRuntime.Get(r.Context(), id.TenantID, serverID)
			if rec == nil {
				rec = &ifaces.ServerRuntimeRecord{
					TenantID: id.TenantID,
					ServerID: serverID,
					Enabled:  snap.Record.Enabled,
				}
			}
			rec.EnvOverrides = envBytes
			if err := d.ServerRuntime.Upsert(r.Context(), rec); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "env_override_failed", err.Error(), nil)
				return
			}
		}
		emitEntityEvent(d, r, audit.EventServerUpdated, "server", serverID, "server.updated",
			map[string]any{"server_id": serverID, "enabled": b.Enabled, "env_overrides": b.EnvOverrides})
		// Re-fetch and respond.
		out, err := d.Registry.Get(r.Context(), id.TenantID, serverID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, snapshotJSON(out))
	}
}

// createAPIServerHandler POST /api/servers — registers via Registry.Apply.
func createAPIServerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Registry == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "registry_unavailable", "registry not configured", nil)
			return
		}
		id, _ := tenant.From(r.Context())
		var spec registry.ServerSpec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		snap, err := d.Registry.Apply(r.Context(), id.TenantID, registry.Mutation{
			Op:      registry.MutOpCreate,
			Server:  &spec,
			ActorID: id.UserID,
		})
		if err != nil {
			var fe *registry.FieldError
			if errors.As(err, &fe) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", fe.Error(),
					map[string]any{"field": fe.Field})
				return
			}
			writeJSONError(w, http.StatusBadRequest, "create_failed", err.Error(), nil)
			return
		}
		emitEntityEvent(d, r, audit.EventServerCreated, "server", spec.ID, "server.created",
			map[string]any{"server_id": spec.ID, "transport": spec.Transport})
		writeJSON(w, http.StatusCreated, snapshotJSON(snap))
	}
}
