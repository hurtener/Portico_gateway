package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// CustomerDTO is the JSON view of a governance Customer.
type CustomerDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	WebhookURL  string `json:"webhook_url,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

func toCustomerDTO(c *ifaces.Customer) CustomerDTO {
	return CustomerDTO{
		ID:          c.ID,
		Name:        c.Name,
		Description: c.Description,
		WebhookURL:  c.WebhookURL,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}
}

// customerCreateRequest is the POST body for creating a Customer.
type customerCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	WebhookURL  string `json:"webhook_url"`
}

// customerUpdateRequest is the PUT body for updating a Customer. All fields
// overwrite the stored row (the URL id is authoritative; body id is ignored).
type customerUpdateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	WebhookURL  string `json:"webhook_url"`
}

// governanceConfigured gates governance Customer/Team CRUD behind the store
// being wired. 503 when nil so a partially-configured build degrades cleanly.
func governanceConfigured(d Deps, w http.ResponseWriter) bool {
	if d.Governance == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "governance_not_configured", "governance store not configured", nil)
		return false
	}
	return true
}

// listCustomersHandler: GET /api/governance/customers.
func listCustomersHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !governanceConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		customers, err := d.Governance.ListCustomers(r.Context(), id.TenantID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}
		out := make([]CustomerDTO, 0, len(customers))
		for _, c := range customers {
			out = append(out, toCustomerDTO(c))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// getCustomerHandler: GET /api/governance/customers/{id}.
func getCustomerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !governanceConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		c, err := d.Governance.GetCustomer(r.Context(), id.TenantID, chi.URLParam(r, "id"))
		if err != nil {
			if errors.Is(err, ifaces.ErrGovernanceNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "customer not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, toCustomerDTO(c))
	}
}

// createCustomerHandler: POST /api/governance/customers. Generates the id
// server-side ("cust_"+randHex16()) and re-Gets to populate timestamps.
func createCustomerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !governanceConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		var req customerCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		if req.Name == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "name is required", nil)
			return
		}
		customerID := "cust_" + randHex16()
		c := &ifaces.Customer{
			TenantID:    id.TenantID,
			ID:          customerID,
			Name:        req.Name,
			Description: req.Description,
			WebhookURL:  req.WebhookURL,
		}
		if err := d.Governance.PutCustomer(r.Context(), c); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "create_failed", err.Error(), nil)
			return
		}
		created, err := d.Governance.GetCustomer(r.Context(), id.TenantID, customerID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "get_after_create_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusCreated, toCustomerDTO(created))
	}
}

// updateCustomerHandler: PUT /api/governance/customers/{id}. Get-then-overwrite-
// then-Put; 404 when the row does not exist.
func updateCustomerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !governanceConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		customerID := chi.URLParam(r, "id")
		existing, err := d.Governance.GetCustomer(r.Context(), id.TenantID, customerID)
		if err != nil {
			if errors.Is(err, ifaces.ErrGovernanceNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "customer not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}
		var req customerUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
			return
		}
		// Overwrite name/description/webhook_url from the body; preserve id +
		// tenant + created_at (the store keeps created_at on conflict).
		if req.Name != "" {
			existing.Name = req.Name
		}
		existing.Description = req.Description
		existing.WebhookURL = req.WebhookURL
		if err := d.Governance.PutCustomer(r.Context(), existing); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "update_failed", err.Error(), nil)
			return
		}
		updated, err := d.Governance.GetCustomer(r.Context(), id.TenantID, customerID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "get_after_update_failed", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusOK, toCustomerDTO(updated))
	}
}

// deleteCustomerHandler: DELETE /api/governance/customers/{id}. 204 on success;
// ErrGovernanceNotFound → 404.
func deleteCustomerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !governanceConfigured(d, w) {
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireGovernanceAdmin(w, id) {
			return
		}
		if err := d.Governance.DeleteCustomer(r.Context(), id.TenantID, chi.URLParam(r, "id")); err != nil {
			if errors.Is(err, ifaces.ErrGovernanceNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "customer not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error(), nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
