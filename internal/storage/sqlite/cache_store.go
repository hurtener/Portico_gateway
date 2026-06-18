package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// cacheEntryStore is the SQLite-backed ifaces.CacheEntryStore — the dev/test
// semantic-cache driver wraps it. Tenant-scoped (§6); cross-tenant collisions
// are impossible by PK construction (tenant_id leads the primary key).
type cacheEntryStore struct {
	db *sql.DB
}

func (s *cacheEntryStore) PutCacheEntry(ctx context.Context, e *ifaces.CacheEntry) error {
	if e == nil {
		return errors.New("sqlite: nil cache entry")
	}
	if e.TenantID == "" || e.CacheKey == "" {
		return errors.New("sqlite: cache entry requires tenant_id and cache_key")
	}
	if e.CreatedAt == "" || e.ExpiresAt == "" {
		return errors.New("sqlite: cache entry requires created_at and expires_at")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO llm_cache_entries(
			tenant_id, cache_key, mode, alias, payload, embedding, similarity,
			tokens, cost_usd, created_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, cache_key) DO UPDATE SET
			mode       = excluded.mode,
			alias      = excluded.alias,
			payload    = excluded.payload,
			embedding  = excluded.embedding,
			similarity = excluded.similarity,
			tokens     = excluded.tokens,
			cost_usd   = excluded.cost_usd,
			created_at = excluded.created_at,
			expires_at = excluded.expires_at
	`, e.TenantID, e.CacheKey, e.Mode, e.Alias, e.Payload, nullBytes(e.Embedding),
		e.Similarity, e.Tokens, e.CostUSD, e.CreatedAt, e.ExpiresAt)
	if err != nil {
		return fmt.Errorf("sqlite: put cache entry: %w", err)
	}
	return nil
}

func (s *cacheEntryStore) GetCacheEntry(ctx context.Context, tenantID, cacheKey string) (*ifaces.CacheEntry, error) {
	if tenantID == "" || cacheKey == "" {
		return nil, errors.New("sqlite: get cache entry requires tenant_id and cache_key")
	}
	var (
		e   ifaces.CacheEntry
		emb []byte
		sim sql.NullFloat64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, cache_key, mode, alias, payload, embedding, similarity, tokens, cost_usd, created_at, expires_at
		FROM llm_cache_entries WHERE tenant_id = ? AND cache_key = ?
	`, tenantID, cacheKey).Scan(
		&e.TenantID, &e.CacheKey, &e.Mode, &e.Alias, &e.Payload, &emb, &sim,
		&e.Tokens, &e.CostUSD, &e.CreatedAt, &e.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ifaces.ErrCacheEntryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: get cache entry: %w", err)
	}
	e.Embedding = emb
	e.Similarity = sim.Float64
	return &e, nil
}

func (s *cacheEntryStore) DeleteByCacheKeyPrefix(ctx context.Context, tenantID, prefix string) (int, error) {
	if tenantID == "" {
		return 0, errors.New("sqlite: delete by cache-key prefix requires tenant_id")
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM llm_cache_entries WHERE tenant_id = ? AND cache_key LIKE ? || '%'
	`, tenantID, prefix)
	if err != nil {
		return 0, fmt.Errorf("sqlite: delete cache by prefix: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *cacheEntryStore) DeleteByAlias(ctx context.Context, tenantID, alias string) (int, error) {
	if tenantID == "" || alias == "" {
		return 0, errors.New("sqlite: delete by alias requires tenant_id and alias")
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM llm_cache_entries WHERE tenant_id = ? AND alias = ?
	`, tenantID, alias)
	if err != nil {
		return 0, fmt.Errorf("sqlite: delete cache by alias: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *cacheEntryStore) DeleteExpired(ctx context.Context, tenantID, nowRFC3339 string) (int, error) {
	if tenantID == "" || nowRFC3339 == "" {
		return 0, errors.New("sqlite: delete expired requires tenant_id and now")
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM llm_cache_entries WHERE tenant_id = ? AND expires_at <= ?
	`, tenantID, nowRFC3339)
	if err != nil {
		return 0, fmt.Errorf("sqlite: delete expired cache: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *cacheEntryStore) CountEntries(ctx context.Context, tenantID string) (int, error) {
	if tenantID == "" {
		return 0, errors.New("sqlite: count entries requires tenant_id")
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM llm_cache_entries WHERE tenant_id = ?
	`, tenantID).Scan(&n); err != nil {
		return 0, fmt.Errorf("sqlite: count cache entries: %w", err)
	}
	return n, nil
}

// nullBytes maps a nil/empty slice to a NULL column value so the optional
// embedding column stores NULL rather than an empty blob.
func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
