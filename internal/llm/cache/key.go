package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"
)

// keySep separates the structured fields of a composed cache key. \x1f (ASCII
// unit separator) never appears in tenant ids / aliases / hex digests, so the
// composed key parses unambiguously and prefix matches are exact.
const keySep = "\x1f"

// Compose builds the deterministic string key for an entry. The tenant id is
// ALWAYS first so cross-tenant collisions are impossible and a tenant's entries
// share a common prefix. Layout:
//
//	<tenant>\x1f<scope>\x1f<scopeID>\x1f<alias>\x1f<hex(sha256(input||extraSalt))>
func Compose(k ifaces.Key) string {
	h := sha256.New()
	h.Write(k.NormalizedInput)
	h.Write(k.ExtraSalt)
	digest := hex.EncodeToString(h.Sum(nil))
	return k.TenantID + keySep +
		string(k.Scope) + keySep +
		k.ScopeID + keySep +
		k.Alias + keySep +
		digest
}

// KeyPrefixForScope returns the composed-key prefix shared by every entry in a
// (tenant, scope, scopeID). Used for invalidation by scope id (e.g. a VK).
func KeyPrefixForScope(tenantID string, scope ifaces.Scope, scopeID string) string {
	return tenantID + keySep + string(scope) + keySep + scopeID + keySep
}

// KeyPrefixForTenant returns the prefix shared by every entry in a tenant.
func KeyPrefixForTenant(tenantID string) string {
	return tenantID + keySep
}

// NormMessage is a normalized chat message. Local type so this package does not
// import the engine / server wire types.
type NormMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// normChatInput is the canonical, stable shape hashed for the cache key. Field
// order is fixed by the struct so two logically-identical requests hash equal.
type normChatInput struct {
	Model       string        `json:"model"`
	Messages    []NormMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
}

// NormalizeChatInput returns canonical bytes for a chat request. Marshalling a
// fixed-field struct yields a stable byte sequence; identical requests produce
// identical bytes (and thus identical hashes), different requests differ.
func NormalizeChatInput(model string, messages []NormMessage, temperature *float64, maxTokens *int) []byte {
	b, _ := json.Marshal(normChatInput{
		Model:       model,
		Messages:    messages,
		Temperature: temperature,
		MaxTokens:   maxTokens,
	})
	return b
}

// NormalizeEmbeddingInput returns canonical bytes for an embedding request.
func NormalizeEmbeddingInput(model string, input []string) []byte {
	b, _ := json.Marshal(struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}{Model: model, Input: input})
	return b
}
