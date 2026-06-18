package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func putCacheEntry(t *testing.T, store ifaces.CacheEntryStore, tenantID, key, alias string, embedding []byte, expiresAt string) {
	t.Helper()
	if err := store.PutCacheEntry(context.Background(), &ifaces.CacheEntry{
		TenantID: tenantID, CacheKey: key, Mode: "exact", Alias: alias,
		Payload: []byte(`{"ok":true}`), Embedding: embedding, Similarity: 1.0,
		Tokens: 42, CostUSD: 0.001, CreatedAt: "2026-05-12T13:00:00Z", ExpiresAt: expiresAt,
	}); err != nil {
		t.Fatalf("put cache entry %s: %v", key, err)
	}
}

func TestCacheStore_RoundTrip_NilAndNonNilEmbedding(t *testing.T) {
	db := open(t)
	store := db.CacheEntries()
	ctx := context.Background()

	putCacheEntry(t, store, "t", "tenant\x1fvk\x1fvk-1\x1fgpt-4\x1fhash1", "gpt-4", nil, "2099-01-01T00:00:00Z")
	got, err := store.GetCacheEntry(ctx, "t", "tenant\x1fvk\x1fvk-1\x1fgpt-4\x1fhash1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Alias != "gpt-4" || got.Tokens != 42 || got.CostUSD != 0.001 || string(got.Payload) != `{"ok":true}` {
		t.Errorf("entry mismatch: %+v", got)
	}
	if len(got.Embedding) != 0 {
		t.Errorf("nil embedding should round-trip empty, got %v", got.Embedding)
	}

	putCacheEntry(t, store, "t", "k-emb", "gpt-4", []byte{1, 2, 3, 4}, "2099-01-01T00:00:00Z")
	got, err = store.GetCacheEntry(ctx, "t", "k-emb")
	if err != nil {
		t.Fatalf("get emb: %v", err)
	}
	if string(got.Embedding) != string([]byte{1, 2, 3, 4}) {
		t.Errorf("embedding not round-tripped: %v", got.Embedding)
	}

	if _, err := store.GetCacheEntry(ctx, "t", "missing"); !errors.Is(err, ifaces.ErrCacheEntryNotFound) {
		t.Fatalf("want ErrCacheEntryNotFound, got %v", err)
	}
}

func TestCacheStore_DeleteByAliasAndPrefix(t *testing.T) {
	db := open(t)
	store := db.CacheEntries()
	ctx := context.Background()

	putCacheEntry(t, store, "t", "tenant\x1fvk\x1fvk-1\x1fgpt-4\x1fa", "gpt-4", nil, "2099-01-01T00:00:00Z")
	putCacheEntry(t, store, "t", "tenant\x1fvk\x1fvk-1\x1fgpt-4\x1fb", "gpt-4", nil, "2099-01-01T00:00:00Z")
	putCacheEntry(t, store, "t", "tenant\x1fvk\x1fvk-2\x1fclaude\x1fc", "claude", nil, "2099-01-01T00:00:00Z")

	n, err := store.DeleteByAlias(ctx, "t", "gpt-4")
	if err != nil {
		t.Fatalf("delete by alias: %v", err)
	}
	if n != 2 {
		t.Errorf("delete by alias count: want 2, got %d", n)
	}
	if cnt, _ := store.CountEntries(ctx, "t"); cnt != 1 {
		t.Errorf("after alias delete: want 1 left, got %d", cnt)
	}

	// Prefix delete on the remaining vk-2 entry.
	n, err = store.DeleteByCacheKeyPrefix(ctx, "t", "tenant\x1fvk\x1fvk-2")
	if err != nil {
		t.Fatalf("delete by prefix: %v", err)
	}
	if n != 1 {
		t.Errorf("delete by prefix count: want 1, got %d", n)
	}
	if cnt, _ := store.CountEntries(ctx, "t"); cnt != 0 {
		t.Errorf("after prefix delete: want 0 left, got %d", cnt)
	}
}

func TestCacheStore_DeleteExpired(t *testing.T) {
	db := open(t)
	store := db.CacheEntries()
	ctx := context.Background()

	putCacheEntry(t, store, "t", "live", "a", nil, "2099-01-01T00:00:00Z")
	putCacheEntry(t, store, "t", "stale", "a", nil, "2000-01-01T00:00:00Z")

	n, err := store.DeleteExpired(ctx, "t", "2026-05-12T13:00:00Z")
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if n != 1 {
		t.Errorf("delete expired count: want 1, got %d", n)
	}
	if _, err := store.GetCacheEntry(ctx, "t", "stale"); !errors.Is(err, ifaces.ErrCacheEntryNotFound) {
		t.Errorf("stale entry should be gone")
	}
	if _, err := store.GetCacheEntry(ctx, "t", "live"); err != nil {
		t.Errorf("live entry should remain: %v", err)
	}
}

func TestCacheStore_TenantIsolation(t *testing.T) {
	db := open(t)
	store := db.CacheEntries()
	ctx := context.Background()

	// Same cache_key string under two tenants — never cross.
	putCacheEntry(t, store, "tenant-a", "same-key", "gpt-4", nil, "2099-01-01T00:00:00Z")
	putCacheEntry(t, store, "tenant-b", "same-key", "gpt-4", nil, "2099-01-01T00:00:00Z")

	if _, err := store.GetCacheEntry(ctx, "tenant-a", "same-key"); err != nil {
		t.Fatalf("tenant-a get: %v", err)
	}
	// Deleting tenant-a's alias must not touch tenant-b.
	if _, err := store.DeleteByAlias(ctx, "tenant-a", "gpt-4"); err != nil {
		t.Fatalf("delete a: %v", err)
	}
	if cnt, _ := store.CountEntries(ctx, "tenant-b"); cnt != 1 {
		t.Errorf("tenant-b entry was affected by tenant-a delete: count=%d", cnt)
	}
}
