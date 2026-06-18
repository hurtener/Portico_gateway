package profiles

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// benchStore returns a distinct bound profile per subject so the resolver
// populates one cache entry per principal — the hot-path shape the acceptance
// criterion measures (a populated 1024-entry LRU serving cache hits).
type benchStore struct{}

func (benchStore) ResolveJWTBinding(_ context.Context, tenantID, sub string) (*ifaces.AgentProfile, error) {
	return &ifaces.AgentProfile{
		TenantID:            tenantID,
		ID:                  "ap_" + sub,
		Name:                sub,
		AllowedMCPServers:   []string{"github", "jira"},
		AllowedTools:        []string{"github.list_issues"},
		AllowedSkills:       []string{"code-review"},
		AllowedModelAliases: []string{"gpt-4o"},
		Scopes:              []string{"mcp:call"},
	}, nil
}

const benchEntries = 1024

// warmResolver returns a resolver primed with benchEntries cache entries.
func warmResolver(tb testing.TB) (*lruResolver, []Principal) {
	tb.Helper()
	r := NewResolver(benchStore{}, time.Minute, benchEntries).(*lruResolver)
	principals := make([]Principal, benchEntries)
	for i := range principals {
		principals[i] = Principal{TenantID: "t1", Subject: fmt.Sprintf("sub-%d", i)}
		if _, err := r.Resolve(context.Background(), principals[i]); err != nil {
			tb.Fatal(err)
		}
	}
	return r, principals
}

// BenchmarkResolve measures the steady-state cache-hit resolve cost with a full
// 1024-entry LRU. Run with `go test -bench BenchmarkResolve ./internal/profiles/`.
func BenchmarkResolve(b *testing.B) {
	r, principals := warmResolver(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Resolve(ctx, principals[i%benchEntries]); err != nil {
			b.Fatal(err)
		}
	}
}

// TestResolve_P95Overhead is the build gate for acceptance #14: P95 resolver
// overhead ≤ 1 ms with a populated 1024-entry LRU. It samples cache-hit resolve
// latencies and fails the build on regression.
func TestResolve_P95Overhead(t *testing.T) {
	if testing.Short() {
		t.Skip("reason: timing-sensitive; skipped under -short")
	}
	r, principals := warmResolver(t)
	ctx := context.Background()

	const samples = 20000
	lat := make([]time.Duration, samples)
	for i := 0; i < samples; i++ {
		p := principals[i%benchEntries]
		start := time.Now()
		if _, err := r.Resolve(ctx, p); err != nil {
			t.Fatal(err)
		}
		lat[i] = time.Since(start)
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	p95 := lat[int(float64(samples)*0.95)]
	if p95 > time.Millisecond {
		t.Fatalf("resolver P95 overhead %v exceeds 1ms budget (acceptance #14)", p95)
	}
	t.Logf("resolver P95 cache-hit overhead: %v (budget 1ms)", p95)
}
