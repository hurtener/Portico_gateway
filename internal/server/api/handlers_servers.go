package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// listServersHandler GET /v1/servers — tenant-scoped list of registered MCP servers.
func listServersHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Registry == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "registry_unavailable", "registry not configured", nil)
			return
		}
		tenantID, ok := tenantOfRequest(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing tenant", nil)
			return
		}
		snaps, err := d.Registry.List(r.Context(), tenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		// Empty array, not null, when nothing is registered.
		out := make([]map[string]any, 0, len(snaps))
		for _, s := range snaps {
			out = append(out, snapshotJSON(s))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// getServerHandler GET /v1/servers/{id}
func getServerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Registry == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "registry_unavailable", "registry not configured", nil)
			return
		}
		tenantID, ok := tenantOfRequest(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing tenant", nil)
			return
		}
		id := chi.URLParam(r, "id")
		snap, err := d.Registry.Get(r.Context(), tenantID, id)
		if err != nil {
			if IsErrNotFound(err) {
				writeJSONError(w, http.StatusNotFound, "not_found", "server not found", map[string]any{"id": id})
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		body := snapshotJSON(snap)
		// Detail view also includes the live instance list.
		instances, ierr := d.Registry.ListInstances(r.Context(), tenantID, id)
		if ierr == nil {
			body["instances"] = instances
		}
		writeJSON(w, http.StatusOK, body)
	}
}

// upsertServerHandler POST /v1/servers and PUT /v1/servers/{id}.
// POST creates (returns 201 on new id, 200 on existing); PUT replaces (always 200).
func upsertServerHandler(d Deps, isPut bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Registry == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "registry_unavailable", "registry not configured", nil)
			return
		}
		tenantID, ok := tenantOfRequest(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing tenant", nil)
			return
		}
		var spec registry.ServerSpec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if isPut {
			// URL id must match body id
			pathID := chi.URLParam(r, "id")
			if spec.ID == "" {
				spec.ID = pathID
			} else if spec.ID != pathID {
				writeJSONError(w, http.StatusBadRequest, "id_mismatch",
					"path id and body id must match", map[string]any{"path": pathID, "body": spec.ID})
				return
			}
		}
		_, getErr := d.Registry.Get(r.Context(), tenantID, spec.ID)
		isCreate := IsErrNotFound(getErr)
		if isPut && isCreate {
			writeJSONError(w, http.StatusNotFound, "not_found", "server not found", map[string]any{"id": spec.ID})
			return
		}
		snap, err := d.Registry.Upsert(r.Context(), tenantID, &spec)
		if err != nil {
			var fe *registry.FieldError
			if errors.As(err, &fe) {
				writeJSONError(w, http.StatusBadRequest, "invalid_spec", fe.Error(), map[string]any{"field": fe.Field})
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "upsert_failed", err.Error(), nil)
			return
		}
		status := http.StatusOK
		if isCreate {
			status = http.StatusCreated
		}
		writeJSON(w, status, snapshotJSON(snap))
	}
}

// deleteServerHandler DELETE /v1/servers/{id}
func deleteServerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Registry == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "registry_unavailable", "registry not configured", nil)
			return
		}
		tenantID, ok := tenantOfRequest(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing tenant", nil)
			return
		}
		id := chi.URLParam(r, "id")
		if err := d.Registry.Delete(r.Context(), tenantID, id); err != nil {
			if IsErrNotFound(err) {
				writeJSONError(w, http.StatusNotFound, "not_found", "server not found", map[string]any{"id": id})
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// reloadServerHandler POST /v1/servers/{id}/reload — drains and restarts every
// running instance for this server. The actual drain happens through the
// Manager/Supervisor; the registry just publishes a reload-intent event.
func reloadServerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Registry == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "registry_unavailable", "registry not configured", nil)
			return
		}
		tenantID, ok := tenantOfRequest(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing tenant", nil)
			return
		}
		id := chi.URLParam(r, "id")
		snap, err := d.Registry.Get(r.Context(), tenantID, id)
		if err != nil {
			if IsErrNotFound(err) {
				writeJSONError(w, http.StatusNotFound, "not_found", "server not found", map[string]any{"id": id})
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		// Re-upsert with the same spec to publish a ChangeUpdated event to
		// the supervisor, which interprets it as "drain + restart".
		if _, err := d.Registry.Upsert(r.Context(), tenantID, &snap.Spec); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "reload_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "reloading", "id": id})
	}
}

// enableServerHandler POST /v1/servers/{id}/enable
func enableServerHandler(d Deps, enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Registry == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "registry_unavailable", "registry not configured", nil)
			return
		}
		tenantID, ok := tenantOfRequest(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing tenant", nil)
			return
		}
		id := chi.URLParam(r, "id")
		snap, err := d.Registry.SetEnabled(r.Context(), tenantID, id, enabled)
		if err != nil {
			if IsErrNotFound(err) {
				writeJSONError(w, http.StatusNotFound, "not_found", "server not found", map[string]any{"id": id})
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "set_enabled_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, snapshotJSON(snap))
	}
}

// listInstancesHandler GET /v1/servers/{id}/instances
func listInstancesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Registry == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "registry_unavailable", "registry not configured", nil)
			return
		}
		tenantID, ok := tenantOfRequest(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing tenant", nil)
			return
		}
		id := chi.URLParam(r, "id")
		// 404 on unknown server id, 200 [] on known-but-empty.
		if _, err := d.Registry.Get(r.Context(), tenantID, id); err != nil {
			if IsErrNotFound(err) {
				writeJSONError(w, http.StatusNotFound, "not_found", "server not found", map[string]any{"id": id})
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		instances, err := d.Registry.ListInstances(r.Context(), tenantID, id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		if instances == nil {
			instances = []*ifaces.InstanceRecord{}
		}
		writeJSON(w, http.StatusOK, instances)
	}
}

// snapshotJSON renders a Snapshot for JSON output. We flatten the most-used
// fields to the top level so clients don't have to traverse `.record.*`.
func snapshotJSON(s *registry.Snapshot) map[string]any {
	if s == nil {
		return nil
	}
	r := s.Record
	return map[string]any{
		"id":            r.ID,
		"tenant_id":     r.TenantID,
		"display_name":  r.DisplayName,
		"transport":     r.Transport,
		"runtime_mode":  r.RuntimeMode,
		"status":        r.Status,
		"status_detail": r.StatusDetail,
		"schema_hash":   r.SchemaHash,
		"last_error":    r.LastError,
		"enabled":       r.Enabled,
		"created_at":    r.CreatedAt,
		"updated_at":    r.UpdatedAt,
		"spec":          s.Spec,
	}
}

// tenantOfRequest extracts the tenant ID from the request context. Wraps
// tenant.From for use inside this package.
func tenantOfRequest(r *http.Request) (string, bool) {
	id, ok := tenant.From(r.Context())
	if !ok {
		return "", false
	}
	return id.TenantID, true
}
