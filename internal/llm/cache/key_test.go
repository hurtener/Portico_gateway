package cache_test

import (
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/llm/cache"
	"github.com/hurtener/Portico_gateway/internal/llm/cache/ifaces"
)

func TestNormalizeChatInput_StableAndDistinct(t *testing.T) {
	msgs := []cache.NormMessage{{Role: "user", Content: "hi"}}
	a := cache.NormalizeChatInput("gpt-4", msgs, nil, nil)
	b := cache.NormalizeChatInput("gpt-4", msgs, nil, nil)
	if string(a) != string(b) {
		t.Fatalf("identical requests must normalize equal:\n%s\n%s", a, b)
	}
	c := cache.NormalizeChatInput("gpt-4", []cache.NormMessage{{Role: "user", Content: "bye"}}, nil, nil)
	if string(a) == string(c) {
		t.Fatalf("different content must normalize differently")
	}
	temp := 0.7
	d := cache.NormalizeChatInput("gpt-4", msgs, &temp, nil)
	if string(a) == string(d) {
		t.Fatalf("temperature must affect normalization")
	}
}

func TestCompose_TenantFirstAndIsolated(t *testing.T) {
	in := cache.NormalizeChatInput("gpt-4", []cache.NormMessage{{Role: "user", Content: "hi"}}, nil, nil)
	ka := ifaces.Key{TenantID: "tenant-a", Scope: ifaces.ScopeVK, ScopeID: "vk-1", Alias: "gpt-4", NormalizedInput: in}
	kb := ifaces.Key{TenantID: "tenant-b", Scope: ifaces.ScopeVK, ScopeID: "vk-1", Alias: "gpt-4", NormalizedInput: in}

	ca := cache.Compose(ka)
	cb := cache.Compose(kb)
	if ca == cb {
		t.Fatalf("identical request under different tenants must compose to different keys")
	}
	if !strings.HasPrefix(ca, "tenant-a\x1f") {
		t.Fatalf("composed key must start with tenant id, got %q", ca)
	}
	if !strings.HasPrefix(ca, cache.KeyPrefixForTenant("tenant-a")) {
		t.Fatalf("composed key must carry the tenant prefix")
	}
	if !strings.HasPrefix(ca, cache.KeyPrefixForScope("tenant-a", ifaces.ScopeVK, "vk-1")) {
		t.Fatalf("composed key must carry the scope prefix")
	}
}

func TestCompose_ExtraSaltChangesKey(t *testing.T) {
	in := cache.NormalizeChatInput("gpt-4", []cache.NormMessage{{Role: "user", Content: "hi"}}, nil, nil)
	base := ifaces.Key{TenantID: "t", Scope: ifaces.ScopeTenant, Alias: "gpt-4", NormalizedInput: in}
	salted := base
	salted.ExtraSalt = []byte("system-prompt-fingerprint")
	if cache.Compose(base) == cache.Compose(salted) {
		t.Fatalf("extra salt must change the composed key")
	}
}
