// handlers_sessions_bundle.go owns the Phase 11 inspector REST surface:
//
//   GET  /api/sessions/{sid}/bundle   → JSON Bundle
//   POST /api/sessions/{sid}/export   → tar.gz stream
//   POST /api/sessions/import         → multipart upload, returns ImportResult
//   GET  /api/sessions/imported       → list imported bundles for tenant
//   GET  /api/spans                   → spanstore query (?session_id=…|trace_id=…)
//   GET  /api/audit/search            → FTS-backed audit search
//
// Each handler is a thin marshaler over the typed packages —
// authentication is the tenant middleware, tenant scoping comes from
// the request context, and the underlying stores enforce isolation
// by always filtering on tenant_id.

package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/sessionbundle"
	"github.com/hurtener/Portico_gateway/internal/telemetry/spanstore"
)

// SpanReader is the spanstore query surface the api package needs.
// The full Store interface adds a Put method; we only need reads
// here so the handler can stay decoupled from the persistence path.
type SpanReader interface {
	QueryBySession(ctx context.Context, tenantID, sessionID string) ([]spanstore.Span, error)
	QueryByTrace(ctx context.Context, tenantID, traceID string) ([]spanstore.Span, error)
}

// AuditSearcher is the surface audit/search.go exposes for the FTS
// endpoint.
type AuditSearcher interface {
	Search(ctx context.Context, q audit.SearchQuery) (audit.SearchResult, error)
}

// getSessionBundleHandler implements GET /api/sessions/{sid}/bundle.
//
// Synthetic ids (prefix "imported:") route to the bundle store; live
// session ids route to the loader.
func getSessionBundleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := tenant.MustFrom(r.Context())
		sid := chi.URLParam(r, "sid")
		if sid == "" {
			writeJSONError(w, http.StatusBadRequest, "missing_session_id", "session id required", nil)
			return
		}

		var (
			bundle *sessionbundle.Bundle
			err    error
		)
		if sessionbundle.IsSynthetic(sid) {
			if d.BundleStore == nil {
				writeJSONError(w, http.StatusServiceUnavailable, "imported_bundles_disabled", "bundle store not configured", nil)
				return
			}
			bundle, err = d.BundleStore.LoadImported(r.Context(), id.TenantID, sid)
		} else {
			if d.BundleLoader == nil {
				writeJSONError(w, http.StatusServiceUnavailable, "bundle_loader_disabled", "bundle loader not configured", nil)
				return
			}
			bundle, err = d.BundleLoader.Load(r.Context(), id.TenantID, sid)
		}

		if errors.Is(err, sessionbundle.ErrSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "session_not_found", "session not found", nil)
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "bundle_load_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, bundle)
	}
}

// exportSessionHandler implements POST /api/sessions/{sid}/export.
// Streams the canonical tar.gz to the client.
func exportSessionHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := tenant.MustFrom(r.Context())
		sid := chi.URLParam(r, "sid")
		if sid == "" {
			writeJSONError(w, http.StatusBadRequest, "missing_session_id", "session id required", nil)
			return
		}

		// Imported sessions: serve back the canonical bytes the
		// importer recorded — no need to re-export.
		if sessionbundle.IsSynthetic(sid) {
			if d.BundleStore == nil {
				writeJSONError(w, http.StatusServiceUnavailable, "imported_bundles_disabled", "bundle store not configured", nil)
				return
			}
			blob, err := d.BundleStore.LoadImportedBytes(r.Context(), id.TenantID, sid)
			if errors.Is(err, sessionbundle.ErrSessionNotFound) {
				writeJSONError(w, http.StatusNotFound, "session_not_found", "session not found", nil)
				return
			}
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "bundle_export_failed", err.Error(), nil)
				return
			}
			writeBundleHeaders(w, sid)
			_, _ = w.Write(blob)
			return
		}

		if d.BundleLoader == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "bundle_loader_disabled", "bundle loader not configured", nil)
			return
		}
		bundle, err := d.BundleLoader.Load(r.Context(), id.TenantID, sid)
		if errors.Is(err, sessionbundle.ErrSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "session_not_found", "session not found", nil)
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "bundle_load_failed", err.Error(), nil)
			return
		}
		writeBundleHeaders(w, sid)
		if err := sessionbundle.Export(r.Context(), bundle, w, sessionbundle.ExportOptions{}); err != nil {
			// Headers are already flushed; we can't send a JSON error.
			d.Logger.Warn("bundle export stream failed", "err", err, "session_id", sid)
			return
		}
	}
}

func writeBundleHeaders(w http.ResponseWriter, sid string) {
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.portico-bundle.tar.gz"`, safeFilename(sid)))
}

func safeFilename(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// importBundleHandler implements POST /api/sessions/import. Accepts
// either a raw tar.gz body (Content-Type: application/gzip) or a
// multipart upload with the bundle in the "bundle" file field.
func importBundleHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := tenant.MustFrom(r.Context())
		if d.BundleImporter == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "bundle_importer_disabled", "bundle importer not configured", nil)
			return
		}

		body, contentType := selectBundleBody(r)
		if body == nil {
			writeJSONError(w, http.StatusBadRequest, "bundle_body_missing", "expected gzip body or multipart upload", nil)
			return
		}
		defer body.Close()

		// Cap inbound size before importer touches it.
		limited := http.MaxBytesReader(w, body, sessionbundle.MaxBundleSize+1)
		res, err := d.BundleImporter.Import(r.Context(), id.TenantID, limited)
		if err != nil {
			httpStatus, code := classifyImportErr(err)
			writeJSONError(w, httpStatus, code, err.Error(), map[string]any{"content_type": contentType})
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

func selectBundleBody(r *http.Request) (io.ReadCloser, string) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(sessionbundle.MaxBundleSize + 1); err != nil {
			return nil, ct
		}
		f, _, err := r.FormFile("bundle")
		if err != nil {
			return nil, ct
		}
		return f, ct
	}
	// Default: treat the whole body as the bundle.
	return r.Body, ct
}

func classifyImportErr(err error) (int, string) {
	switch {
	case errors.Is(err, sessionbundle.ErrBundleTooLarge):
		return http.StatusRequestEntityTooLarge, "bundle_too_large"
	case errors.Is(err, sessionbundle.ErrBundleSchema):
		return http.StatusBadRequest, "bundle_schema_mismatch"
	case errors.Is(err, sessionbundle.ErrBundleCorrupt):
		return http.StatusBadRequest, "bundle_corrupt"
	default:
		// Any other parse error (invalid gzip, malformed tar, JSON
		// decode failure) is the operator handing us garbage — return
		// 400 with the closest typed code rather than blaming the
		// server.
		msg := err.Error()
		if strings.Contains(msg, "tar") || strings.Contains(msg, "gzip") || strings.Contains(msg, "json") {
			return http.StatusBadRequest, "bundle_corrupt"
		}
		return http.StatusInternalServerError, "bundle_import_failed"
	}
}

// listImportedHandler implements GET /api/sessions/imported.
func listImportedHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := tenant.MustFrom(r.Context())
		if d.BundleStore == nil {
			writeJSON(w, http.StatusOK, map[string]any{"imported": []any{}})
			return
		}
		rows, err := d.BundleStore.List(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "imported_list_failed", err.Error(), nil)
			return
		}
		if rows == nil {
			rows = []sessionbundle.ImportedRow{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"imported": rows})
	}
}

// listSpansHandler implements GET /api/spans.
func listSpansHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := tenant.MustFrom(r.Context())
		if d.SpanReader == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "spans_disabled", "span store not configured", nil)
			return
		}
		q := r.URL.Query()
		sessionID := q.Get("session_id")
		traceID := q.Get("trace_id")
		var (
			spans []spanstore.Span
			err   error
		)
		switch {
		case sessionID != "":
			spans, err = d.SpanReader.QueryBySession(r.Context(), id.TenantID, sessionID)
		case traceID != "":
			spans, err = d.SpanReader.QueryByTrace(r.Context(), id.TenantID, traceID)
		default:
			writeJSONError(w, http.StatusBadRequest, "missing_filter", "session_id or trace_id required", nil)
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "spans_query_failed", err.Error(), nil)
			return
		}
		if spans == nil {
			spans = []spanstore.Span{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"spans": spans})
	}
}

// auditSearchHandler implements GET /api/audit/search.
func auditSearchHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := tenant.MustFrom(r.Context())
		if d.AuditSearch == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "audit_search_disabled", "audit search not configured", nil)
			return
		}
		params := r.URL.Query()
		sq := audit.SearchQuery{
			TenantID:  id.TenantID,
			Q:         params.Get("q"),
			SessionID: params.Get("session_id"),
			Type:      params.Get("type"),
			Cursor:    params.Get("cursor"),
		}
		if l := params.Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil {
				sq.Limit = n
			}
		}
		if from := params.Get("from"); from != "" {
			if t, err := time.Parse(time.RFC3339, from); err == nil {
				sq.From = t
			}
		}
		if to := params.Get("to"); to != "" {
			if t, err := time.Parse(time.RFC3339, to); err == nil {
				sq.To = t
			}
		}
		res, err := d.AuditSearch.Search(r.Context(), sq)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "audit_search_failed", err.Error(), nil)
			return
		}
		if res.Events == nil {
			res.Events = []audit.Event{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"events":      res.Events,
			"next_cursor": res.Next,
		})
	}
}
