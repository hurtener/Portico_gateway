package api

import (
	"net/http"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/llm/cache"
	cacheifaces "github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"

	// Register the inmem cache driver for these tests.
	_ "github.com/hurtener/Portico_gateway/internal/llm/cache/inmem"
)

func depsWithCache(t *testing.T) (Deps, *fakeLLMEngine) {
	t.Helper()
	d, eng := llmDeps()
	c, err := cache.Open("inmem", nil, cacheifaces.Deps{})
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	d.Cache = c
	d.CacheScope = cacheifaces.ScopeTenant
	return d, eng
}

func chatBody() openAIChatRequest {
	return openAIChatRequest{Model: "gpt-4", Messages: []openAIMessage{{Role: "user", Content: "cache me"}}}
}

func TestChatCompletions_CacheHit(t *testing.T) {
	d, eng := depsWithCache(t)

	// First call: miss → engine dispatched + result stored.
	w1 := runHandler(chatCompletionsHandler(d), newReq("POST", "/v1/chat/completions", chatBody(), ScopeLLMInvoke))
	if w1.Code != http.StatusOK {
		t.Fatalf("first call status %d: %s", w1.Code, w1.Body.String())
	}
	if eng.gotReq.TenantID != "t1" {
		t.Fatalf("engine should have been called on miss")
	}

	// Second identical call: hit → engine NOT called, response from cache.
	eng.gotReq.TenantID = "" // sentinel: stays empty iff the engine is skipped
	w2 := runHandler(chatCompletionsHandler(d), newReq("POST", "/v1/chat/completions", chatBody(), ScopeLLMInvoke))
	if w2.Code != http.StatusOK {
		t.Fatalf("second call status %d: %s", w2.Code, w2.Body.String())
	}
	if h := w2.Header().Get("x-portico-cache"); h != "hit" {
		t.Fatalf("second call should be a cache hit, header=%q", h)
	}
	if eng.gotReq.TenantID != "" {
		t.Fatalf("engine must NOT be called on a cache hit")
	}
	var resp openAIChatResponse
	decodeJSON(t, w2, &resp)
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "hello from the model" {
		t.Fatalf("cached response wrong: %+v", resp.Choices)
	}
}

func TestChatCompletions_CacheNoStore(t *testing.T) {
	d, eng := depsWithCache(t)

	// no-store: dispatch but do NOT cache.
	r := newReq("POST", "/v1/chat/completions", chatBody(), ScopeLLMInvoke)
	r.Header.Set("Cache-Control", "no-store")
	if w := runHandler(chatCompletionsHandler(d), r); w.Code != http.StatusOK {
		t.Fatalf("no-store call status %d", w.Code)
	}

	// A following normal call must MISS (nothing was stored) → engine called.
	eng.gotReq.TenantID = ""
	w := runHandler(chatCompletionsHandler(d), newReq("POST", "/v1/chat/completions", chatBody(), ScopeLLMInvoke))
	if w.Header().Get("x-portico-cache") == "hit" {
		t.Fatalf("no-store must not have populated the cache")
	}
	if eng.gotReq.TenantID != "t1" {
		t.Fatalf("engine should be called after a no-store (uncached) request")
	}
}

func TestChatCompletions_CacheNoReadStillWrites(t *testing.T) {
	d, eng := depsWithCache(t)

	// no-cache: skip the read (force upstream) but still write the result.
	r := newReq("POST", "/v1/chat/completions", chatBody(), ScopeLLMInvoke)
	r.Header.Set("Cache-Control", "no-cache")
	if w := runHandler(chatCompletionsHandler(d), r); w.Code != http.StatusOK {
		t.Fatalf("no-cache call status %d", w.Code)
	}
	if eng.gotReq.TenantID != "t1" {
		t.Fatalf("no-cache should still dispatch upstream")
	}

	// A following normal call must HIT (the no-cache call wrote the entry).
	eng.gotReq.TenantID = ""
	w := runHandler(chatCompletionsHandler(d), newReq("POST", "/v1/chat/completions", chatBody(), ScopeLLMInvoke))
	if w.Header().Get("x-portico-cache") != "hit" {
		t.Fatalf("normal call after no-cache should hit the written entry")
	}
	if eng.gotReq.TenantID != "" {
		t.Fatalf("engine must not be called on the hit")
	}
}

func TestChatCompletions_CacheTenantIsolation(t *testing.T) {
	d, _ := depsWithCache(t)
	// Populate tenant t1's cache.
	runHandler(chatCompletionsHandler(d), newReq("POST", "/v1/chat/completions", chatBody(), ScopeLLMInvoke))

	// A different tenant requesting the same prompt must NOT hit t1's entry. The
	// model store only knows tenant t1, so a t2 request 404s at model resolution
	// — but crucially it never serves t1's cached body. Assert no cross hit by
	// checking the key composition is tenant-partitioned via the cache directly.
	k1 := cacheKeyFor(d, newReq("POST", "/x", nil), "t1", "gpt-4", chatBody().Messages, nil, nil)
	k2 := cacheKeyFor(d, newReq("POST", "/x", nil), "t2", "gpt-4", chatBody().Messages, nil, nil)
	if cache.Compose(k1) == cache.Compose(k2) {
		t.Fatal("cache keys must differ across tenants")
	}
	if _, hit, _ := d.Cache.Lookup(newReq("POST", "/x", nil).Context(), k2, cacheifaces.LookupOpts{}); hit {
		t.Fatal("tenant t2 must not hit tenant t1's cached entry")
	}
}
