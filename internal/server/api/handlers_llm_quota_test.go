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
func (f *fakeQuotaStore) SetQuota(context.Context, *storageifaces.LLMQuota) error { return nil }
func (f *fakeQuotaStore) DeleteQuota(context.Context, string) error               { return nil }

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
