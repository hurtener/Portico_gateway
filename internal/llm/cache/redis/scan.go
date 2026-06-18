package redis

import (
	"context"
)

// scanKeys collects every key matching a Redis glob pattern via SCAN + MATCH
// (never KEYS — KEYS blocks the server). The SCAN cursor iterates until done.
// Returns the keys in an arbitrary order.
func (c *redisCache) scanKeys(ctx context.Context, match string) ([]string, error) {
	var keys []string
	iter := c.client.Scan(ctx, 0, match, scanBatchSize).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}
