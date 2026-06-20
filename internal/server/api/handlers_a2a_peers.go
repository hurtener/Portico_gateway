package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/a2a/ingest"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// A2APeerDTO is the JSON view of a registered A2A peer.
type A2APeerDTO struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Endpoint      string `json:"endpoint"`
	EgressAuthRef string `json:"egress_auth_ref,omitempty"`
	AgentCardJSON string `json:"agent_card_json,omitempty"`
	Enabled       bool   `json:"enabled"`
	CreatedAt     string `json:"created_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

// a2aPeerCreateRequest is the POST body for creating an A2A peer. AgentCardJSON
// is intentionally read-only (populated by the ingestion unit later).
type a2aPeerCreateRequest struct {
	Name          string `json:"name"`
	Endpoint      string `json:"endpoint"`
	EgressAuthRef string `json:"egress_auth_ref"`
	Enabled       *bool  `json:"enabled"`
}

// a2aPeerUpdateRequest is the PUT body for updating an A2A peer. The URL id
// is authoritative; body id is ignored.
type a2aPeerUpdateRequest struct {
	Name          string `json:"name"`
	Endpoint      string `json:"endpoint"`
	EgressAuthRef string `json:"egress_auth_ref"`
	Enabled       *bool  `json:"enabled"`
}

// a2aPeersConfigured gates A2A peer CRUD behind the store being wired. 503
// when nil so a partially-configured build degrades cleanly (mirrors
// governanceConfigured).
func a2aPeersConfigured(d Deps, w http.ResponseWriter) bool {
	if d.A2APeers == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "a2a_not_configured", "a2a peer store not configured", nil)
		return false
	}
	return true
}

// listA2APeersHandler: GET /api/a2a/peers.
func listA2APeersHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a2aPeersConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		peers, err := d.A2APeers.ListPeers(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]A2APeerDTO, 0, len(peers))
		for _, p := range peers {
			out = append(out, toA2APeerDTO(p))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// getA2APeerHandler: GET /api/a2a/peers/{id}.
func getA2APeerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a2aPeersConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		p, err := d.A2APeers.GetPeer(r.Context(), id.TenantID, chi.URLParam(r, "id"))
		if err != nil {
			if errors.Is(err, ifaces.ErrA2APeerNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "a2a peer not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, toA2APeerDTO(p))
	}
}

// createA2APeerHandler: POST /api/a2a/peers. Generates the id server-side
// ("a2a_"+randHex16()) and re-Gets to populate timestamps.
func createA2APeerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a2aPeersConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		var req a2aPeerCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if req.Name == "" || req.Endpoint == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "name and endpoint are required", nil)
			return
		}
		peerID := "a2a_" + randHex16()
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		p := &ifaces.A2APeer{
			TenantID:      id.TenantID,
			ID:            peerID,
			Name:          req.Name,
			Endpoint:      req.Endpoint,
			EgressAuthRef: req.EgressAuthRef,
			Enabled:       enabled,
		}
		if err := d.A2APeers.PutPeer(r.Context(), p); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "create_failed", err.Error(), nil)
			return
		}
		created, err := d.A2APeers.GetPeer(r.Context(), id.TenantID, peerID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "get_after_create_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusCreated, toA2APeerDTO(created))
	}
}

// updateA2APeerHandler: PUT /api/a2a/peers/{id}. Get-then-overwrite-then-Put;
// 404 when the row does not exist. Name/Endpoint are only overwritten when
// non-empty (avoids an empty body wiping them); EgressAuthRef always
// overwrites (allows clearing); Enabled only when the body field is set.
func updateA2APeerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a2aPeersConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		peerID := chi.URLParam(r, "id")
		existing, err := d.A2APeers.GetPeer(r.Context(), id.TenantID, peerID)
		if err != nil {
			if errors.Is(err, ifaces.ErrA2APeerNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "a2a peer not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		var req a2aPeerUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if req.Name != "" {
			existing.Name = req.Name
		}
		if req.Endpoint != "" {
			existing.Endpoint = req.Endpoint
		}
		existing.EgressAuthRef = req.EgressAuthRef
		if req.Enabled != nil {
			existing.Enabled = *req.Enabled
		}
		if err := d.A2APeers.PutPeer(r.Context(), existing); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "update_failed", err.Error(), nil)
			return
		}
		updated, err := d.A2APeers.GetPeer(r.Context(), id.TenantID, peerID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "get_after_update_failed", err.Error(), nil)
			return
		}
		// Drop any cached southbound client so the next dispatch rebuilds with
		// the new endpoint/credentials.
		if d.A2APool != nil {
			d.A2APool.Invalidate(r.Context(), id.TenantID, peerID)
		}
		writeJSON(w, http.StatusOK, toA2APeerDTO(updated))
	}
}

// deleteA2APeerHandler: DELETE /api/a2a/peers/{id}. 204 on success;
// ErrA2APeerNotFound → 404.
func deleteA2APeerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a2aPeersConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		if err := d.A2APeers.DeletePeer(r.Context(), id.TenantID, chi.URLParam(r, "id")); err != nil {
			if errors.Is(err, ifaces.ErrA2APeerNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "a2a peer not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
			return
		}
		if d.A2APool != nil {
			d.A2APool.Invalidate(r.Context(), id.TenantID, chi.URLParam(r, "id"))
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// refreshA2APeerCardHandler: POST /api/a2a/peers/{id}/refresh-card. Fetches the
// peer's agent card from its well-known URL and persists it; 200 + updated DTO.
// 404 unknown peer; 502 when the peer is unreachable / returns a bad card.
func refreshA2APeerCardHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a2aPeersConfigured(d, w) {
			return
		}
		if d.A2ACardRefresher == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "a2a_not_configured", "a2a card refresher not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		peer, err := d.A2ACardRefresher.RefreshCard(r.Context(), id.TenantID, chi.URLParam(r, "id"))
		if err != nil {
			switch {
			case errors.Is(err, ifaces.ErrA2APeerNotFound):
				writeJSONError(w, http.StatusNotFound, "not_found", "a2a peer not found", nil)
			case errors.Is(err, ingest.ErrCardFetch):
				writeJSONError(w, http.StatusBadGateway, "a2a_card_fetch_failed", err.Error(), nil)
			default:
				writeJSONError(w, http.StatusInternalServerError, "refresh_failed", err.Error(), nil)
			}
			return
		}
		writeJSON(w, http.StatusOK, toA2APeerDTO(peer))
	}
}

func toA2APeerDTO(p *ifaces.A2APeer) A2APeerDTO {
	return A2APeerDTO{
		ID:            p.ID,
		Name:          p.Name,
		Endpoint:      p.Endpoint,
		EgressAuthRef: p.EgressAuthRef,
		AgentCardJSON: p.AgentCardJSON,
		Enabled:       p.Enabled,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}
