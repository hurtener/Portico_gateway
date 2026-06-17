package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"

	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// --- functional map-backed fakes for CRUD ---

type fakeProvCRUD struct {
	provs map[string]*storageifaces.LLMProvider
	keys  map[string][]*storageifaces.LLMProviderKey
}

func newFakeProvCRUD() *fakeProvCRUD {
	return &fakeProvCRUD{provs: map[string]*storageifaces.LLMProvider{}, keys: map[string][]*storageifaces.LLMProviderKey{}}
}

func (f *fakeProvCRUD) CreateProvider(_ context.Context, p *storageifaces.LLMProvider) error {
	f.provs[p.Name] = p
	return nil
}
func (f *fakeProvCRUD) GetProvider(_ context.Context, tenantID, name string) (*storageifaces.LLMProvider, error) {
	if p, ok := f.provs[name]; ok && p.TenantID == tenantID {
		return p, nil
	}
	return nil, storageifaces.ErrLLMProviderNotFound
}
func (f *fakeProvCRUD) ListProviders(_ context.Context, tenantID string) ([]*storageifaces.LLMProvider, error) {
	out := []*storageifaces.LLMProvider{}
	for _, p := range f.provs {
		if p.TenantID == tenantID {
			out = append(out, p)
		}
	}
	return out, nil
}
func (f *fakeProvCRUD) UpdateProvider(_ context.Context, p *storageifaces.LLMProvider) error {
	if _, ok := f.provs[p.Name]; !ok {
		return storageifaces.ErrLLMProviderNotFound
	}
	f.provs[p.Name] = p
	return nil
}
func (f *fakeProvCRUD) DeleteProvider(_ context.Context, _, name string) error {
	if _, ok := f.provs[name]; !ok {
		return storageifaces.ErrLLMProviderNotFound
	}
	delete(f.provs, name)
	return nil
}
func (f *fakeProvCRUD) AddKey(_ context.Context, k *storageifaces.LLMProviderKey) error {
	f.keys[k.ProviderName] = append(f.keys[k.ProviderName], k)
	return nil
}
func (f *fakeProvCRUD) ListKeys(_ context.Context, _, providerName string) ([]*storageifaces.LLMProviderKey, error) {
	return f.keys[providerName], nil
}
func (f *fakeProvCRUD) DeleteKey(_ context.Context, _, providerName, keyID string) error {
	ks := f.keys[providerName]
	for i, k := range ks {
		if k.KeyID == keyID {
			f.keys[providerName] = append(ks[:i], ks[i+1:]...)
			return nil
		}
	}
	return storageifaces.ErrLLMProviderNotFound
}

type fakeModelCRUD struct {
	models map[string]*storageifaces.LLMModel
}

func newFakeModelCRUD() *fakeModelCRUD {
	return &fakeModelCRUD{models: map[string]*storageifaces.LLMModel{}}
}

func (f *fakeModelCRUD) CreateModel(_ context.Context, m *storageifaces.LLMModel) error {
	f.models[m.Alias] = m
	return nil
}
func (f *fakeModelCRUD) GetModel(_ context.Context, tenantID, alias string) (*storageifaces.LLMModel, error) {
	if m, ok := f.models[alias]; ok && m.TenantID == tenantID {
		return m, nil
	}
	return nil, storageifaces.ErrLLMModelNotFound
}
func (f *fakeModelCRUD) ListModels(_ context.Context, tenantID string) ([]*storageifaces.LLMModel, error) {
	out := []*storageifaces.LLMModel{}
	for _, m := range f.models {
		if m.TenantID == tenantID {
			out = append(out, m)
		}
	}
	return out, nil
}
func (f *fakeModelCRUD) UpdateModel(_ context.Context, m *storageifaces.LLMModel) error {
	if _, ok := f.models[m.Alias]; !ok {
		return storageifaces.ErrLLMModelNotFound
	}
	f.models[m.Alias] = m
	return nil
}
func (f *fakeModelCRUD) DeleteModel(_ context.Context, _, alias string) error {
	if _, ok := f.models[alias]; !ok {
		return storageifaces.ErrLLMModelNotFound
	}
	delete(f.models, alias)
	return nil
}

func crudDeps() Deps {
	return Deps{Logger: llmTestLogger(), LLMProviders: newFakeProvCRUD(), LLMModels: newFakeModelCRUD()}
}

// --- provider CRUD tests ---

func TestLLMProviderCRUD_RoundTrip(t *testing.T) {
	d := crudDeps()

	// create
	body := llmProviderDTO{Name: "primary", Driver: "openai", Config: map[string]any{"base_url": "https://x"}, Enabled: true}
	w := runHandler(upsertLLMProviderHandler(d, false), newReq("POST", "/api/llm/providers", body, "admin"))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", w.Code, w.Body.String())
	}

	// get
	r := withChiURLParam(newReq("GET", "/api/llm/providers/primary", nil, "admin"), "name", "primary")
	w = runHandler(getLLMProviderHandler(d), r)
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d", w.Code)
	}
	var got llmProviderDTO
	decodeJSON(t, w, &got)
	if got.Driver != "openai" || got.Config["base_url"] != "https://x" {
		t.Errorf("get mismatch: %+v", got)
	}

	// list
	w = runHandler(listLLMProvidersHandler(d), newReq("GET", "/api/llm/providers", nil, "admin"))
	var list struct {
		Providers []llmProviderDTO `json:"providers"`
	}
	decodeJSON(t, w, &list)
	if len(list.Providers) != 1 {
		t.Errorf("list: %+v", list)
	}

	// update
	body.Enabled = false
	r = withChiURLParam(newReq("PUT", "/api/llm/providers/primary", body, "admin"), "name", "primary")
	w = runHandler(upsertLLMProviderHandler(d, true), r)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d", w.Code)
	}

	// keys: add → list → delete
	kbody := llmProviderKeyDTO{CredentialRef: "vault:k1", Weight: 2, ModelAllowlist: []string{"gpt-4"}, Enabled: true}
	r = withChiURLParam(newReq("POST", "/api/llm/providers/primary/keys", kbody, "admin"), "name", "primary")
	w = runHandler(addLLMProviderKeyHandler(d), r)
	if w.Code != http.StatusCreated {
		t.Fatalf("add key: %d body=%s", w.Code, w.Body.String())
	}
	var key llmProviderKeyDTO
	decodeJSON(t, w, &key)
	if key.KeyID == "" {
		t.Error("key id should be auto-generated")
	}
	r = withChiURLParam(newReq("GET", "/api/llm/providers/primary/keys", nil, "admin"), "name", "primary")
	w = runHandler(listLLMProviderKeysHandler(d), r)
	var keys struct {
		Keys []llmProviderKeyDTO `json:"keys"`
	}
	decodeJSON(t, w, &keys)
	if len(keys.Keys) != 1 || keys.Keys[0].Weight != 2 {
		t.Errorf("list keys: %+v", keys)
	}
	r = newReq("DELETE", "/api/llm/providers/primary/keys/"+key.KeyID, nil, "admin")
	r = withChiURLParam(withChiURLParam(r, "name", "primary"), "keyID", key.KeyID)
	w = runHandler(deleteLLMProviderKeyHandler(d), r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete key: %d", w.Code)
	}

	// delete provider → then get 404
	r = withChiURLParam(newReq("DELETE", "/api/llm/providers/primary", nil, "admin"), "name", "primary")
	w = runHandler(deleteLLMProviderHandler(d), r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
	r = withChiURLParam(newReq("GET", "/api/llm/providers/primary", nil, "admin"), "name", "primary")
	w = runHandler(getLLMProviderHandler(d), r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: %d", w.Code)
	}
}

func TestLLMProviderCRUD_RequiresAdmin(t *testing.T) {
	d := crudDeps()
	body := llmProviderDTO{Name: "p", Driver: "openai", Enabled: true}
	w := runHandler(upsertLLMProviderHandler(d, false), newReq("POST", "/api/llm/providers", body, "some:other"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestLLMProviderCRUD_InvalidJSON(t *testing.T) {
	d := crudDeps()
	r := httptest.NewRequest("POST", "/api/llm/providers", strings.NewReader("{not json"))
	r = r.WithContext(tenant.With(r.Context(), tenant.Identity{TenantID: "t1", Scopes: []string{"admin"}}))
	w := runHandler(upsertLLMProviderHandler(d, false), r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- model CRUD tests ---

func TestLLMModelCRUD_RoundTrip(t *testing.T) {
	d := crudDeps()
	body := llmModelDTO{Alias: "gpt-4", ProviderName: "primary", ProviderModel: "gpt-4o",
		Capabilities: []string{"chat"}, Enabled: true}
	w := runHandler(upsertLLMModelHandler(d, false), newReq("POST", "/api/llm/models", body, "admin"))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d body=%s", w.Code, w.Body.String())
	}
	r := withChiURLParam(newReq("GET", "/api/llm/models/gpt-4", nil, "admin"), "alias", "gpt-4")
	w = runHandler(getLLMModelHandler(d), r)
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d", w.Code)
	}
	var got llmModelDTO
	decodeJSON(t, w, &got)
	if got.ProviderModel != "gpt-4o" || len(got.Capabilities) != 1 || got.Capabilities[0] != "chat" {
		t.Errorf("get mismatch: %+v", got)
	}

	body.ProviderModel = "gpt-4o-mini"
	r = withChiURLParam(newReq("PUT", "/api/llm/models/gpt-4", body, "admin"), "alias", "gpt-4")
	if w = runHandler(upsertLLMModelHandler(d, true), r); w.Code != http.StatusOK {
		t.Fatalf("update: %d", w.Code)
	}

	r = withChiURLParam(newReq("DELETE", "/api/llm/models/gpt-4", nil, "admin"), "alias", "gpt-4")
	if w = runHandler(deleteLLMModelHandler(d), r); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", w.Code)
	}
	r = withChiURLParam(newReq("GET", "/api/llm/models/gpt-4", nil, "admin"), "alias", "gpt-4")
	if w = runHandler(getLLMModelHandler(d), r); w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: %d", w.Code)
	}
}

func TestLLMModelCRUD_MissingFields(t *testing.T) {
	d := crudDeps()
	w := runHandler(upsertLLMModelHandler(d, false),
		newReq("POST", "/api/llm/models", llmModelDTO{Alias: "x"}, "admin"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
