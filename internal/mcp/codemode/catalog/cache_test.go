package catalog

import (
	"sync"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
)

func TestProjectionCache_HitReturnsSameContent(t *testing.T) {
	c := NewProjectionCache(8)
	snap := sampleSnapshot()
	a := c.Get(snap, BindingServer)
	b := c.Get(snap, BindingServer)
	if len(a.Files) != len(b.Files) {
		t.Fatalf("cache returned different projections")
	}
	for p, content := range a.Files {
		if b.Files[p] != content {
			t.Errorf("cache content drift on %s", p)
		}
	}
}

func TestProjectionCache_SeparateLevelsCachedIndependently(t *testing.T) {
	c := NewProjectionCache(8)
	snap := sampleSnapshot()
	srv := c.Get(snap, BindingServer)
	tool := c.Get(snap, BindingTool)
	if len(tool.Files) <= len(srv.Files) {
		t.Errorf("tool level should have more files than server level: %d vs %d", len(tool.Files), len(srv.Files))
	}
}

func TestProjectionCache_Invalidate(t *testing.T) {
	c := NewProjectionCache(8)
	snap := sampleSnapshot()
	c.Get(snap, BindingServer)
	c.Invalidate(snap.ID)
	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n != 0 {
		t.Errorf("invalidate left %d entries", n)
	}
}

func TestProjectionCache_NilSnapshotNotCached(t *testing.T) {
	c := NewProjectionCache(8)
	_ = c.Get(nil, BindingServer)
	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n != 0 {
		t.Errorf("nil snapshot should not be cached, got %d entries", n)
	}
}

func TestProjectionCache_FIFOEviction(t *testing.T) {
	c := NewProjectionCache(2)
	for _, id := range []string{"s1", "s2", "s3"} {
		c.Get(&snapshots.Snapshot{ID: id, Tools: sampleSnapshot().Tools}, BindingServer)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) > 2 {
		t.Errorf("cache exceeded max: %d entries", len(c.entries))
	}
	if _, ok := c.entries["s1|server"]; ok {
		t.Errorf("oldest entry s1 should have been evicted")
	}
}

func TestProjectionCache_ConcurrentAccess(t *testing.T) {
	c := NewProjectionCache(16)
	snap := sampleSnapshot()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.Get(snap, BindingServer)
		}()
	}
	wg.Wait()
}
