// Phase 8: REST handlers for /api/skills/authored. Tenant-scoped via
// tenant.MustFrom(ctx). Mutations require JWT scope skills:write
// (enforced at the router layer).

package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
	"github.com/hurtener/Portico_gateway/internal/skills/source/authored"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// AuthoredSkillsController is the slim contract the API depends on.
// Concrete impl is *authored.Store; declared as an interface so tests
// can stub.
type AuthoredSkillsController interface {
	ListAuthored(ctx context.Context, tenantID string) ([]authored.Authored, error)
	GetAuthored(ctx context.Context, tenantID, skillID, version string) (*authored.Authored, error)
	History(ctx context.Context, tenantID, skillID string) ([]authored.Authored, error)
	GetActive(ctx context.Context, tenantID, skillID string) (*authored.Authored, error)
	CreateDraft(ctx context.Context, tenantID, userID string, m manifest.Manifest, files []authored.File) (*authored.Authored, error)
	UpdateDraft(ctx context.Context, tenantID, skillID, version, userID string, m manifest.Manifest, files []authored.File) (*authored.Authored, error)
	Publish(ctx context.Context, tenantID, skillID, version string) (*authored.Authored, error)
	Archive(ctx context.Context, tenantID, skillID, version string) error
	DeleteDraft(ctx context.Context, tenantID, skillID, version string) error
}

// SkillValidator is the slim contract for the /api/skills/validate
// endpoint. The concrete impl wraps the loader's validation pipeline.
type SkillValidator interface {
	Validate(body []byte) []ValidatorViolation
}

// ValidatorViolation mirrors the loader's Violation. Lifted into the
// api package to avoid an import cycle.
type ValidatorViolation struct {
	Pointer string `json:"pointer"`
	Line    int    `json:"line,omitempty"`
	Col     int    `json:"col,omitempty"`
	Reason  string `json:"reason"`
	Kind    string `json:"kind,omitempty"`
}

// listAuthoredHandler implements GET /api/skills/authored.
func listAuthoredHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.AuthoredSkills
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "authored_unavailable", "authored skills store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		rows, err := c.ListAuthored(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": authoredListDTO(rows)})
	}
}

// getAuthoredActiveHandler implements GET /api/skills/authored/{id}.
func getAuthoredActiveHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.AuthoredSkills
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "authored_unavailable", "authored skills store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		skillID := chi.URLParam(r, "id")
		rec, err := c.GetActive(r.Context(), id.TenantID, skillID)
		if err != nil {
			if errors.Is(err, ifaces.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "no active version", map[string]any{"skill_id": skillID})
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, authoredDetailDTO(*rec))
	}
}

// historyAuthoredHandler implements GET /api/skills/authored/{id}/versions.
func historyAuthoredHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.AuthoredSkills
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "authored_unavailable", "authored skills store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		skillID := chi.URLParam(r, "id")
		rows, err := c.History(r.Context(), id.TenantID, skillID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "history_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": authoredListDTO(rows)})
	}
}

// getAuthoredVersionHandler implements GET /api/skills/authored/{id}/versions/{v}.
func getAuthoredVersionHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.AuthoredSkills
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "authored_unavailable", "authored skills store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		skillID := chi.URLParam(r, "id")
		version := chi.URLParam(r, "v")
		rec, err := c.GetAuthored(r.Context(), id.TenantID, skillID, version)
		if err != nil {
			if errors.Is(err, ifaces.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "version not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, authoredDetailDTO(*rec))
	}
}

// createAuthoredHandler implements POST /api/skills/authored.
func createAuthoredHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.AuthoredSkills
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "authored_unavailable", "authored skills store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		body, err := readAuthoredRequest(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_body", err.Error(), nil)
			return
		}
		m, validation, err := decodeAuthoredManifest(body.Manifest)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "manifest_invalid", err.Error(), map[string]any{"violations": validation})
			return
		}
		files := authoredFilesFromDTO(body.Files)
		rec, err := c.CreateDraft(r.Context(), id.TenantID, id.UserID, m, files)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "create_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusCreated, authoredDetailDTO(*rec))
	}
}

// updateAuthoredHandler implements PUT /api/skills/authored/{id}/versions/{v}.
func updateAuthoredHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.AuthoredSkills
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "authored_unavailable", "authored skills store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		skillID := chi.URLParam(r, "id")
		version := chi.URLParam(r, "v")
		body, err := readAuthoredRequest(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_body", err.Error(), nil)
			return
		}
		m, validation, err := decodeAuthoredManifest(body.Manifest)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "manifest_invalid", err.Error(), map[string]any{"violations": validation})
			return
		}
		files := authoredFilesFromDTO(body.Files)
		rec, err := c.UpdateDraft(r.Context(), id.TenantID, skillID, version, id.UserID, m, files)
		if err != nil {
			if errors.Is(err, ifaces.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "draft version missing", nil)
				return
			}
			writeJSONError(w, http.StatusBadRequest, "update_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, authoredDetailDTO(*rec))
	}
}

// publishAuthoredHandler implements POST /api/skills/authored/{id}/versions/{v}/publish.
func publishAuthoredHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.AuthoredSkills
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "authored_unavailable", "authored skills store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		skillID := chi.URLParam(r, "id")
		version := chi.URLParam(r, "v")
		rec, err := c.Publish(r.Context(), id.TenantID, skillID, version)
		if err != nil {
			if errors.Is(err, ifaces.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "draft version missing", nil)
				return
			}
			writeJSONError(w, http.StatusBadRequest, "publish_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, authoredDetailDTO(*rec))
	}
}

// archiveAuthoredHandler implements POST /api/skills/authored/{id}/versions/{v}/archive.
func archiveAuthoredHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.AuthoredSkills
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "authored_unavailable", "authored skills store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		skillID := chi.URLParam(r, "id")
		version := chi.URLParam(r, "v")
		if err := c.Archive(r.Context(), id.TenantID, skillID, version); err != nil {
			if errors.Is(err, ifaces.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "version missing", nil)
				return
			}
			writeJSONError(w, http.StatusBadRequest, "archive_failed", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// deleteAuthoredDraftHandler implements DELETE /api/skills/authored/{id}/versions/{v}.
func deleteAuthoredDraftHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := d.AuthoredSkills
		if c == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "authored_unavailable", "authored skills store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		skillID := chi.URLParam(r, "id")
		version := chi.URLParam(r, "v")
		if err := c.DeleteDraft(r.Context(), id.TenantID, skillID, version); err != nil {
			if errors.Is(err, ifaces.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "draft missing", nil)
				return
			}
			writeJSONError(w, http.StatusBadRequest, "delete_failed", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// validateSkillHandler implements POST /api/skills/validate.
// Body shape: { "manifest": "<yaml string>" } OR
// { "manifest": "<yaml>", "files": [...] }.
// Response carries violations + a content checksum.
func validateSkillHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.SkillValidator == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "validator_unavailable", "skill validator not configured", nil)
			return
		}
		_ = tenant.MustFrom(r.Context())
		body, err := readAuthoredRequest(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_body", err.Error(), nil)
			return
		}
		violations := d.SkillValidator.Validate([]byte(body.Manifest))
		writeJSON(w, http.StatusOK, map[string]any{
			"valid":      len(violations) == 0,
			"violations": violations,
			"checksum":   "", // computed by the authoring UI on the client side; reserved
			"validated":  time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// --- helpers --------------------------------------------------------

type authoredRequest struct {
	Manifest string             `json:"manifest"`
	Files    []authoredFileDTO  `json:"files,omitempty"`
}

type authoredFileDTO struct {
	RelPath  string `json:"relpath"`
	MIMEType string `json:"mime_type,omitempty"`
	Body     string `json:"body"`
}

type authoredDTO struct {
	SkillID      string `json:"skill_id"`
	Version      string `json:"version"`
	Status       string `json:"status"`
	Title        string `json:"title,omitempty"`
	Description  string `json:"description,omitempty"`
	Checksum     string `json:"checksum"`
	AuthorUserID string `json:"author_user_id,omitempty"`
	CreatedAt    string `json:"created_at"`
	PublishedAt  string `json:"published_at,omitempty"`
}

type authoredDetail struct {
	authoredDTO
	Manifest map[string]any    `json:"manifest"`
	Files    []authoredFileOut `json:"files"`
}

type authoredFileOut struct {
	RelPath  string `json:"relpath"`
	MIMEType string `json:"mime_type"`
	Body     string `json:"body"`
}

func readAuthoredRequest(r *http.Request) (authoredRequest, error) {
	if r.Body == nil {
		return authoredRequest{}, errors.New("empty body")
	}
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		return authoredRequest{}, err
	}
	var body authoredRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		return authoredRequest{}, err
	}
	return body, nil
}

func decodeAuthoredManifest(body string) (manifest.Manifest, []ValidatorViolation, error) {
	if body == "" {
		return manifest.Manifest{}, []ValidatorViolation{{Reason: "manifest body is empty"}}, errors.New("manifest body is empty")
	}
	m, _, err := manifest.Parse([]byte(body))
	if err != nil {
		return manifest.Manifest{}, []ValidatorViolation{{Reason: err.Error(), Kind: "schema"}}, err
	}
	return *m, nil, nil
}

func authoredFilesFromDTO(in []authoredFileDTO) []authored.File {
	out := make([]authored.File, 0, len(in))
	for _, f := range in {
		out = append(out, authored.File{
			RelPath:  f.RelPath,
			MIMEType: f.MIMEType,
			Body:     []byte(f.Body),
		})
	}
	return out
}

func authoredToDTO(a authored.Authored) authoredDTO {
	dto := authoredDTO{
		SkillID:      a.SkillID,
		Version:      a.Version,
		Status:       a.Status,
		Title:        a.Manifest.Title,
		Description:  a.Manifest.Description,
		Checksum:     a.Checksum,
		AuthorUserID: a.AuthorUserID,
		CreatedAt:    a.CreatedAt.Format(time.RFC3339),
	}
	if a.PublishedAt != nil {
		dto.PublishedAt = a.PublishedAt.Format(time.RFC3339)
	}
	return dto
}

func authoredListDTO(rows []authored.Authored) []authoredDTO {
	out := make([]authoredDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, authoredToDTO(r))
	}
	return out
}

func authoredDetailDTO(a authored.Authored) authoredDetail {
	mraw := map[string]any{}
	_ = json.Unmarshal(a.ManifestRaw, &mraw)
	files := make([]authoredFileOut, 0, len(a.Files))
	for _, f := range a.Files {
		files = append(files, authoredFileOut{
			RelPath:  f.RelPath,
			MIMEType: f.MIMEType,
			Body:     string(f.Body),
		})
	}
	return authoredDetail{
		authoredDTO: authoredToDTO(a),
		Manifest:    mraw,
		Files:       files,
	}
}
