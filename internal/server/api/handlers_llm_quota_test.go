package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/llm/quota"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type fakeQuotaStore struct{ q storageifaces.LLMQuota }

func (f *fakeQuotaStore) GetQuota(context.Context, string) (*storageifaces.LLMQuota, error) {
	c := f.q
	return &c, nil
}
func (f *fakeQuotaStore) GetOrDefault(context.Context, string) (*storageifaces.LLMQuota, error) {
	c := f.q
	return &c, nil
}
func (f *fakeQuotaStore) SetQuota(_ context.Context, q *storageifaces.LLMQuota) error {
	f.q = *q
	return nil
}
func (f *fakeQuotaStore) DeleteQuota(context.Context, string) error { return nil }

func TestChatCompletions_QuotaExceeded(t *testing.T) {
	d, _ := llmDeps()
	d.LLMQuota = quota.NewEnforcer()
	d.LLMQuotas = &fakeQuotaStore{q: storageifaces.LLMQuota{TenantID: "t1", RequestsPerMinute: 1}}

	body := openAIChatRequest{Model: "gpt-4", Messages: []openAIMessage{{Role: "user", Content: "hi"}}}

	// First request is allowed.
	w := runHandler(chatCompletionsHandler(d), newReq("POST", "/v1/chat/completions", body, ScopeLLMInvoke))
	if w.Code != http.StatusOK {
		t.Fatalf("req 1: expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	// Second request exceeds the 1/min request quota.
	w = runHandler(chatCompletionsHandler(d), newReq("POST", "/v1/chat/completions", body, ScopeLLMInvoke))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("req 2: expected 429, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Error   string         `json:"error"`
		Details map[string]any `json:"details"`
	}
	decodeJSON(t, w, &resp)
	if resp.Error != "quota_exceeded" {
		t.Errorf("expected quota_exceeded, got %q", resp.Error)
	}
	if resp.Details["limit"] != "requests_per_minute" {
		t.Errorf("expected limit=requests_per_minute, got %v", resp.Details["limit"])
	}
}

func TestGetLLMQuota_ReturnsTenantRow(t *testing.T) {
	d, _ := llmDeps()
	d.LLMQuotas = &fakeQuotaStore{q: storageifaces.LLMQuota{
		TenantID: "t1", RequestsPerMinute: 42, TokensPerMinute: 1000, TokensPerDay: 2000, CostUSDPerDay: 5,
	}}

	w := runHandler(getLLMQuotaHandler(d), newReq("GET", "/api/llm/quota", nil, ScopeLLMInvoke))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp llmQuotaDTO
	decodeJSON(t, w, &resp)
	if resp.RequestsPerMinute != 42 || resp.TokensPerMinute != 1000 || resp.CostUSDPerDay != 5 {
		t.Errorf("unexpected quota DTO: %+v", resp)
	}
}

func TestPutLLMQuota_AdminUpserts(t *testing.T) {
	d, _ := llmDeps()
	store := &fakeQuotaStore{q: storageifaces.LLMQuota{TenantID: "t1"}}
	d.LLMQuotas = store

	body := llmQuotaDTO{RequestsPerMinute: 10, TokensPerMinute: 500, TokensPerDay: 9000, CostUSDPerDay: 12.5}
	w := runHandler(putLLMQuotaHandler(d), newReq("PUT", "/api/llm/quota", body, "admin"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if store.q.RequestsPerMinute != 10 || store.q.TokensPerDay != 9000 || store.q.CostUSDPerDay != 12.5 {
		t.Errorf("store not updated: %+v", store.q)
	}
}

func TestPutLLMQuota_RequiresAdmin(t *testing.T) {
	d, _ := llmDeps()
	d.LLMQuotas = &fakeQuotaStore{q: storageifaces.LLMQuota{TenantID: "t1"}}

	body := llmQuotaDTO{RequestsPerMinute: 10}
	w := runHandler(putLLMQuotaHandler(d), newReq("PUT", "/api/llm/quota", body, ScopeLLMInvoke))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without admin, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPutLLMQuota_RejectsNegative(t *testing.T) {
	d, _ := llmDeps()
	d.LLMQuotas = &fakeQuotaStore{q: storageifaces.LLMQuota{TenantID: "t1"}}

	body := llmQuotaDTO{RequestsPerMinute: -1}
	w := runHandler(putLLMQuotaHandler(d), newReq("PUT", "/api/llm/quota", body, "admin"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative value, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestChatCompletions_NoQuotaWiring_Unlimited(t *testing.T) {
	// When the enforcer/store are nil, enforcement is skipped (no 429).
	d, _ := llmDeps()
	body := openAIChatRequest{Model: "gpt-4", Messages: []openAIMessage{{Role: "user", Content: "hi"}}}
	for i := 0; i < 5; i++ {
		w := runHandler(chatCompletionsHandler(d), newReq("POST", "/v1/chat/completions", body, ScopeLLMInvoke))
		if w.Code != http.StatusOK {
			t.Fatalf("req %d: expected 200 (unlimited), got %d", i, w.Code)
		}
	}
}
