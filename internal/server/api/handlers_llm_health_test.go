package api

import (
	"context"
	"net/http"
	"testing"

	engineifaces "github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// healthyEngine embeds the gateway fake and reports a configurable driver-health
// view so the health handler's cross-reference logic can be exercised.
type healthyEngine struct {
	*fakeLLMEngine
	health []engineifaces.ProviderHealth
}

func (h *healthyEngine) Health(context.Context) ([]engineifaces.ProviderHealth, error) {
	return h.health, nil
}

func TestGetLLMHealth_CrossReferencesEngineAndProviders(t *testing.T) {
	d, eng := llmDeps()
	d.LLMEngine = &healthyEngine{
		fakeLLMEngine: eng,
		health: []engineifaces.ProviderHealth{
			{Provider: "openai", Driver: "openai", Healthy: true, Detail: "configured"},
		},
	}
	// Three providers: an enabled openai (healthy), a disabled openai (reported
	// unhealthy/disabled), and an enabled provider on an unknown driver.
	d.LLMProviders = &fakeProvStore{provs: map[string]*storageifaces.LLMProvider{
		"prod":       {TenantID: "t1", Name: "prod", Driver: "openai", Enabled: true},
		"staging":    {TenantID: "t1", Name: "staging", Driver: "openai", Enabled: false},
		"experiment": {TenantID: "t1", Name: "experiment", Driver: "mystery", Enabled: true},
	}}

	w := runHandler(getLLMHealthHandler(d), newReq("GET", "/api/llm/health", nil, ScopeLLMInvoke))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Providers []llmProviderHealthDTO `json:"providers"`
	}
	decodeJSON(t, w, &resp)
	if len(resp.Providers) != 3 {
		t.Fatalf("expected 3 providers, got %d", len(resp.Providers))
	}
	byName := map[string]llmProviderHealthDTO{}
	for _, p := range resp.Providers {
		byName[p.Name] = p
	}
	if !byName["prod"].Healthy {
		t.Errorf("prod should be healthy: %+v", byName["prod"])
	}
	if byName["staging"].Healthy || byName["staging"].Detail != "disabled" {
		t.Errorf("staging (disabled) should be unhealthy/disabled: %+v", byName["staging"])
	}
	if byName["experiment"].Healthy || byName["experiment"].Detail != "driver not loaded in engine" {
		t.Errorf("experiment (unknown driver) should be unhealthy: %+v", byName["experiment"])
	}
}

func TestGetLLMHealth_NotConfigured(t *testing.T) {
	d, _ := llmDeps()
	d.LLMEngine = nil // engine missing
	w := runHandler(getLLMHealthHandler(d), newReq("GET", "/api/llm/health", nil, ScopeLLMInvoke))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when engine absent, got %d", w.Code)
	}
}
