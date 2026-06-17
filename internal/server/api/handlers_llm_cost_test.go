package api

import (
	"context"
	"net/http"
	"testing"

	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// fakeCostStore is an in-memory LLMCostStore for handler tests.
type fakeCostStore struct {
	units map[string]*storageifaces.LLMUnitCost // keyed by driver|model
	daily []*storageifaces.LLMCostDaily
}

func newFakeCostStore() *fakeCostStore {
	return &fakeCostStore{units: map[string]*storageifaces.LLMUnitCost{}}
}

func costKey(driver, model string) string { return driver + "|" + model }

func (f *fakeCostStore) SetUnitCost(_ context.Context, c *storageifaces.LLMUnitCost) error {
	cp := *c
	f.units[costKey(c.ProviderDriver, c.ProviderModel)] = &cp
	return nil
}

func (f *fakeCostStore) GetUnitCost(_ context.Context, driver, model string) (*storageifaces.LLMUnitCost, error) {
	if c, ok := f.units[costKey(driver, model)]; ok {
		return c, nil
	}
	return nil, storageifaces.ErrLLMUnitCostNotFound
}

func (f *fakeCostStore) ListUnitCosts(context.Context) ([]*storageifaces.LLMUnitCost, error) {
	out := make([]*storageifaces.LLMUnitCost, 0, len(f.units))
	for _, c := range f.units {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeCostStore) AddUsage(_ context.Context, tenantID, day, alias string, requests, inputTok, outputTok int, costUSD float64) error {
	for _, row := range f.daily {
		if row.TenantID == tenantID && row.Day == day && row.Alias == alias {
			row.Requests += requests
			row.InputTok += inputTok
			row.OutputTok += outputTok
			row.CostUSD += costUSD
			return nil
		}
	}
	f.daily = append(f.daily, &storageifaces.LLMCostDaily{
		TenantID: tenantID, Day: day, Alias: alias,
		Requests: requests, InputTok: inputTok, OutputTok: outputTok, CostUSD: costUSD,
	})
	return nil
}

func (f *fakeCostStore) ListDaily(_ context.Context, tenantID, _, _ string) ([]*storageifaces.LLMCostDaily, error) {
	out := make([]*storageifaces.LLMCostDaily, 0)
	for _, row := range f.daily {
		if row.TenantID == tenantID {
			out = append(out, row)
		}
	}
	return out, nil
}

func TestChatCompletions_RecordsCost(t *testing.T) {
	d, _ := llmDeps()
	costs := newFakeCostStore()
	// Price the upstream model: $1/1k input, $2/1k output.
	_ = costs.SetUnitCost(context.Background(), &storageifaces.LLMUnitCost{
		ProviderDriver: "openai", ProviderModel: "gpt-4o", InputPer1K: 1, OutputPer1K: 2,
	})
	d.LLMCosts = costs

	body := openAIChatRequest{Model: "gpt-4", Messages: []openAIMessage{{Role: "user", Content: "hi"}}}
	w := runHandler(chatCompletionsHandler(d), newReq("POST", "/v1/chat/completions", body, ScopeLLMInvoke))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if len(costs.daily) != 1 {
		t.Fatalf("expected 1 daily row, got %d", len(costs.daily))
	}
	row := costs.daily[0]
	// 5 input tok * $1/1k + 3 output tok * $2/1k = 0.005 + 0.006 = 0.011
	if row.Alias != "gpt-4" || row.Requests != 1 || row.InputTok != 5 || row.OutputTok != 3 {
		t.Errorf("rollup wrong: %+v", row)
	}
	if row.CostUSD < 0.0109 || row.CostUSD > 0.0111 {
		t.Errorf("cost = %v, want ~0.011", row.CostUSD)
	}
}

func TestChatCompletions_RecordsUsage_WhenUnpriced(t *testing.T) {
	d, _ := llmDeps()
	costs := newFakeCostStore() // no prices set
	d.LLMCosts = costs

	body := openAIChatRequest{Model: "gpt-4", Messages: []openAIMessage{{Role: "user", Content: "hi"}}}
	runHandler(chatCompletionsHandler(d), newReq("POST", "/v1/chat/completions", body, ScopeLLMInvoke))
	if len(costs.daily) != 1 || costs.daily[0].Requests != 1 {
		t.Fatalf("expected usage recorded even when unpriced: %+v", costs.daily)
	}
	if costs.daily[0].CostUSD != 0 {
		t.Errorf("unpriced cost should be 0, got %v", costs.daily[0].CostUSD)
	}
}

func TestListLLMCosts_SummarizesRange(t *testing.T) {
	d, _ := llmDeps()
	costs := newFakeCostStore()
	_ = costs.AddUsage(context.Background(), "t1", "2026-06-16", "gpt-4", 2, 100, 50, 0.5)
	_ = costs.AddUsage(context.Background(), "t1", "2026-06-17", "gpt-4", 1, 40, 20, 0.25)
	d.LLMCosts = costs

	w := runHandler(listLLMCostsHandler(d), newReq("GET", "/api/llm/costs", nil, ScopeLLMInvoke))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp llmCostsResponseDTO
	decodeJSON(t, w, &resp)
	if len(resp.Daily) != 2 {
		t.Fatalf("expected 2 daily rows, got %d", len(resp.Daily))
	}
	if resp.Summary.Requests != 3 || resp.Summary.InputTok != 140 || resp.Summary.CostUSD != 0.75 {
		t.Errorf("summary wrong: %+v", resp.Summary)
	}
}

func TestListLLMCosts_RejectsBadDate(t *testing.T) {
	d, _ := llmDeps()
	d.LLMCosts = newFakeCostStore()
	w := runHandler(listLLMCostsHandler(d), newReq("GET", "/api/llm/costs?from=not-a-date", nil, ScopeLLMInvoke))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad date, got %d", w.Code)
	}
}

func TestPutLLMPrice_AdminUpserts(t *testing.T) {
	d, _ := llmDeps()
	costs := newFakeCostStore()
	d.LLMCosts = costs

	body := llmUnitCostDTO{ProviderDriver: "openai", ProviderModel: "gpt-4o", InputPer1K: 2.5, OutputPer1K: 10}
	w := runHandler(putLLMPriceHandler(d), newReq("PUT", "/api/llm/costs/prices", body, "admin"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	got, err := costs.GetUnitCost(context.Background(), "openai", "gpt-4o")
	if err != nil || got.InputPer1K != 2.5 || got.OutputPer1K != 10 {
		t.Errorf("price not stored: %+v err=%v", got, err)
	}
}

func TestPutLLMPrice_RequiresAdmin(t *testing.T) {
	d, _ := llmDeps()
	d.LLMCosts = newFakeCostStore()
	body := llmUnitCostDTO{ProviderDriver: "openai", ProviderModel: "gpt-4o", InputPer1K: 1}
	w := runHandler(putLLMPriceHandler(d), newReq("PUT", "/api/llm/costs/prices", body, ScopeLLMInvoke))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without admin, got %d", w.Code)
	}
}
