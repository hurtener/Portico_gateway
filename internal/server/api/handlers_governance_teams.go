package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// TeamDTO is the JSON view of a governance Team.
type TeamDTO struct {
	ID          string `json:"id"`
	CustomerID  string `json:"customer_id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

func toTeamDTO(tm *ifaces.Team) TeamDTO {
	return TeamDTO{
		ID:          tm.ID,
		CustomerID:  tm.CustomerID,
		Name:        tm.Name,
		Description: tm.Description,
		CreatedAt:   tm.CreatedAt,
		UpdatedAt:   tm.UpdatedAt,
	}
}

// teamCreateRequest is the POST body for creating a Team. customer_id is
// optional ("" → standalone under the tenant).
type teamCreateRequest struct {
	Name        string `json:"name"`
	CustomerID  string `json:"customer_id"`
	Description string `json:"description"`
}

// teamUpdateRequest is the PUT body for updating a Team.
type teamUpdateRequest struct {
	Name        string `json:"name"`
	CustomerID  string `json:"customer_id"`
	Description string `json:"description"`
}

// listTeamsHandler: GET /api/governance/teams.
func listTeamsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !governanceConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		teams, err := d.Governance.ListTeams(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]TeamDTO, 0, len(teams))
		for _, tm := range teams {
			out = append(out, toTeamDTO(tm))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// getTeamHandler: GET /api/governance/teams/{id}.
func getTeamHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !governanceConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		tm, err := d.Governance.GetTeam(r.Context(), id.TenantID, chi.URLParam(r, "id"))
		if err != nil {
			if errors.Is(err, ifaces.ErrGovernanceNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "team not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, toTeamDTO(tm))
	}
}

// createTeamsHandler: POST /api/governance/teams. Generates the id server-side
// ("team_"+randHex16()) and re-Gets to populate timestamps.
func createTeamsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !governanceConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		var req teamCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if req.Name == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "name is required", nil)
			return
		}
		teamID := "team_" + randHex16()
		tm := &ifaces.Team{
			TenantID:    id.TenantID,
			ID:          teamID,
			CustomerID:  req.CustomerID,
			Name:        req.Name,
			Description: req.Description,
		}
		if err := d.Governance.PutTeam(r.Context(), tm); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "create_failed", err.Error(), nil)
			return
		}
		created, err := d.Governance.GetTeam(r.Context(), id.TenantID, teamID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "get_after_create_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusCreated, toTeamDTO(created))
	}
}

// updateTeamHandler: PUT /api/governance/teams/{id}. Get-then-overwrite-then-Put.
func updateTeamHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !governanceConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		teamID := chi.URLParam(r, "id")
		existing, err := d.Governance.GetTeam(r.Context(), id.TenantID, teamID)
		if err != nil {
			if errors.Is(err, ifaces.ErrGovernanceNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "team not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		var req teamUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if req.Name != "" {
			existing.Name = req.Name
		}
		// customer_id may be cleared by sending "" in the body.
		existing.CustomerID = req.CustomerID
		existing.Description = req.Description
		if err := d.Governance.PutTeam(r.Context(), existing); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "update_failed", err.Error(), nil)
			return
		}
		updated, err := d.Governance.GetTeam(r.Context(), id.TenantID, teamID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "get_after_update_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, toTeamDTO(updated))
	}
}

// deleteTeamHandler: DELETE /api/governance/teams/{id}. 204 on success.
func deleteTeamHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !governanceConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		if err := d.Governance.DeleteTeam(r.Context(), id.TenantID, chi.URLParam(r, "id")); err != nil {
			if errors.Is(err, ifaces.ErrGovernanceNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "team not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
