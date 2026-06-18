package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	virtualkeys "github.com/hurtener/Portico_gateway/internal/auth/virtual_keys"
	"github.com/hurtener/Portico_gateway/internal/llm/cache"
	cacheifaces "github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"
)

// encodeChatResponse / decodeChatResponse (de)serialise the cached payload. The
// stored bytes are the OpenAI response the redactor already shaped before audit.
func encodeChatResponse(resp openAIChatResponse) ([]byte, bool) {
	b, err := json.Marshal(resp)
	return b, err == nil
}

func decodeChatResponse(payload []byte) (openAIChatResponse, bool) {
	var resp openAIChatResponse
	err := json.Unmarshal(payload, &resp)
	return resp, err == nil
}

// responseHasToolCalls reports whether a response carries tool calls (which are
// conversation-state-dependent and must never be cached, acceptance #8). The
// current gateway wire type does not model tool calls, so this is always false;
// it is the single gate to update when tool-use lands.
func responseHasToolCalls(_ openAIChatResponse) bool { return false }

// cacheBypass captures a request's cache-control intent. no-store = neither read
// nor write; no-cache = skip the read (force upstream) but still write the result.
// Recognises the standard Cache-Control header plus Bifrost's x-bf-cache-* for
// compatibility.
type cacheBypass struct {
	noStore bool
	noRead  bool
}

func parseCacheBypass(r *http.Request) cacheBypass {
	cc := strings.ToLower(r.Header.Get("Cache-Control"))
	b := cacheBypass{}
	if strings.Contains(cc, "no-store") {
		b.noStore = true
		b.noRead = true
	}
	if strings.Contains(cc, "no-cache") {
		b.noRead = true
	}
	// Bifrost-compat headers.
	if v := strings.ToLower(r.Header.Get("x-bf-cache-no-store")); v == "true" || v == "1" {
		b.noStore = true
		b.noRead = true
	}
	if v := strings.ToLower(r.Header.Get("x-bf-cache-no-cache")); v == "true" || v == "1" {
		b.noRead = true
	}
	return b
}

// cacheKeyFor builds the cache key for a chat request: tenant-partitioned, with
// the configured scope (tenant|vk) narrowing within the tenant.
func cacheKeyFor(d Deps, r *http.Request, tenantID, alias string, msgs []openAIMessage, temp *float64, maxTok *int) cacheifaces.Key {
	scope := d.CacheScope
	if scope == "" {
		scope = cacheifaces.ScopeTenant
	}
	scopeID := ""
	if scope == cacheifaces.ScopeVK {
		if vk, ok := virtualkeys.FromContext(r.Context()); ok {
			scopeID = vk.VKID
		} else {
			// No VK on the request — fall back to tenant scope so a JWT caller
			// still gets a stable (tenant-wide) key.
			scope = cacheifaces.ScopeTenant
		}
	}
	norm := make([]cache.NormMessage, 0, len(msgs))
	for _, m := range msgs {
		norm = append(norm, cache.NormMessage{Role: m.Role, Content: m.Content})
	}
	return cacheifaces.Key{
		TenantID:        tenantID,
		Scope:           scope,
		ScopeID:         scopeID,
		Alias:           alias,
		Mode:            cacheifaces.ModeExact,
		NormalizedInput: cache.NormalizeChatInput(alias, norm, temp, maxTok),
	}
}

// serveChatFromCache attempts a cache read for a non-streaming chat request. On a
// hit it writes the cached OpenAI response (with cache_hit telemetry) + audits
// llm.cache_hit and returns true. Returns false on miss, bypass, or when no
// cache is configured. Tool-use and streaming requests are never cache-read here
// (the caller gates streaming; this gates no-read bypass).
func serveChatFromCache(d Deps, w http.ResponseWriter, r *http.Request, tenantID string, req openAIChatRequest) bool {
	if d.Cache == nil || req.Stream {
		return false
	}
	if parseCacheBypass(r).noRead {
		return false
	}
	key := cacheKeyFor(d, r, tenantID, req.Model, req.Messages, req.Temperature, req.MaxTokens)
	entry, hit, err := d.Cache.Lookup(r.Context(), key, cacheifaces.LookupOpts{Threshold: d.CacheThreshold})
	if err != nil || !hit || entry == nil {
		return false
	}
	// Defence in depth: the key is tenant-first, but never serve a foreign entry.
	resp, ok := decodeChatResponse(entry.Payload)
	if !ok {
		return false
	}
	resp.Model = req.Model
	emitWithActor(d, r, "llm.cache_hit", tenantID, map[string]any{
		"alias": req.Model, "mode": string(entry.Mode), "age_s": int(time.Since(entry.CreatedAt).Seconds()),
	})
	w.Header().Set("x-portico-cache", "hit")
	writeJSON(w, http.StatusOK, resp)
	return true
}

// storeChatInCache writes a completed non-streaming, non-tool-use response to the
// cache (best-effort). Skipped on no-store bypass, streaming, or tool-use (those
// responses are conversation-state-dependent and unsafe to replay).
func storeChatInCache(d Deps, r *http.Request, tenantID string, req openAIChatRequest, resp openAIChatResponse, costUSD float64) {
	if d.Cache == nil || req.Stream {
		return
	}
	if parseCacheBypass(r).noStore {
		return
	}
	if responseHasToolCalls(resp) {
		return
	}
	payload, ok := encodeChatResponse(resp)
	if !ok {
		return
	}
	ttl := d.CacheTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	key := cacheKeyFor(d, r, tenantID, req.Model, req.Messages, req.Temperature, req.MaxTokens)
	now := time.Now()
	_ = d.Cache.Store(r.Context(), key, cacheifaces.Entry{
		Payload:   payload,
		Mode:      cacheifaces.ModeExact,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
		Tokens:    resp.Usage.TotalTokens,
		CostUSD:   costUSD,
	})
}
