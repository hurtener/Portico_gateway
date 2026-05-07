package source

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/skills/manifest"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// inMemSourceStore is a simple in-process SkillSourceStore for tests.
type inMemSourceStore struct {
	rows []*ifaces.SkillSourceRecord
}

func (s *inMemSourceStore) Upsert(_ context.Context, r *ifaces.SkillSourceRecord) error {
	for i, row := range s.rows {
		if row.TenantID == r.TenantID && row.Name == r.Name {
			s.rows[i] = r
			return nil
		}
	}
	s.rows = append(s.rows, r)
	return nil
}

func (s *inMemSourceStore) Get(_ context.Context, tenantID, name string) (*ifaces.SkillSourceRecord, error) {
	for _, r := range s.rows {
		if r.TenantID == tenantID && r.Name == name {
			return r, nil
		}
	}
	return nil, ifaces.ErrNotFound
}

func (s *inMemSourceStore) List(_ context.Context, tenantID string) ([]*ifaces.SkillSourceRecord, error) {
	out := make([]*ifaces.SkillSourceRecord, 0)
	for _, r := range s.rows {
		if r.TenantID == tenantID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *inMemSourceStore) Delete(_ context.Context, tenantID, name string) error {
	for i, r := range s.rows {
		if r.TenantID == tenantID && r.Name == name {
			s.rows = append(s.rows[:i], s.rows[i+1:]...)
			return nil
		}
	}
	return ifaces.ErrNotFound
}

func (s *inMemSourceStore) MarkRefreshed(_ context.Context, _, _ string, _ time.Time, _ string) error {
	return nil
}

// stubFactory is a Source factory used to inspect priority ordering.
type stubFactory struct{ name string }

func (f *stubFactory) Build(_ context.Context, _ []byte, deps FactoryDeps) (Source, error) {
	return &stubSource{name: deps.SourceName}, nil
}

type stubSource struct{ name string }

func (s *stubSource) Name() string { return s.name }
func (s *stubSource) List(_ context.Context) ([]Ref, error) {
	return []Ref{{ID: "x", Source: s.name, Loc: "stub"}}, nil
}
func (s *stubSource) Open(_ context.Context, _ Ref) (manifest.Manifest, error) {
	return manifest.Manifest{}, nil
}
func (s *stubSource) ReadFile(_ context.Context, _ Ref, _ string) (io.ReadCloser, ContentInfo, error) {
	return nil, ContentInfo{}, io.EOF
}
func (s *stubSource) Watch(_ context.Context) (<-chan Event, error) { return nil, nil }

func TestRegistry_OrdersByPriority(t *testing.T) {
	store := &inMemSourceStore{}
	ctx := context.Background()
	// Two stub drivers with distinct names so register doesn't panic
	// across test runs (subtests in different files share the global
	// driver registry).
	if !registered("stub1") {
		Register("stub1", (&stubFactory{name: "stub1"}).Build)
	}
	if !registered("stub2") {
		Register("stub2", (&stubFactory{name: "stub2"}).Build)
	}
	cfgA, _ := json.Marshal(map[string]string{})
	store.rows = append(store.rows,
		&ifaces.SkillSourceRecord{TenantID: "t1", Name: "low", Driver: "stub1", ConfigJSON: cfgA, Priority: 50, Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		&ifaces.SkillSourceRecord{TenantID: "t1", Name: "high", Driver: "stub2", ConfigJSON: cfgA, Priority: 10, Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	)
	reg := NewRegistry(store, nil, nil, t.TempDir(), discardLog(), nil)
	srcs, err := reg.Sources(ctx, "t1")
	if err != nil {
		t.Fatalf("Sources: %v", err)
	}
	if len(srcs) != 2 {
		t.Fatalf("len=%d", len(srcs))
	}
	if srcs[0].Name() != "high" || srcs[1].Name() != "low" {
		t.Errorf("priority order wrong: %v", srcNames(srcs))
	}
}

func TestRegistry_Invalidate_RebuildsCache(t *testing.T) {
	store := &inMemSourceStore{}
	ctx := context.Background()
	if !registered("stub3") {
		Register("stub3", (&stubFactory{name: "stub3"}).Build)
	}
	cfgA, _ := json.Marshal(map[string]string{})
	store.rows = append(store.rows,
		&ifaces.SkillSourceRecord{TenantID: "t1", Name: "first", Driver: "stub3", ConfigJSON: cfgA, Priority: 100, Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	)
	reg := NewRegistry(store, nil, nil, t.TempDir(), discardLog(), nil)
	srcs, _ := reg.Sources(ctx, "t1")
	if len(srcs) != 1 {
		t.Fatalf("expected 1 source, got %d", len(srcs))
	}
	store.rows = store.rows[:0]
	reg.Invalidate("t1")
	srcs, _ = reg.Sources(ctx, "t1")
	if len(srcs) != 0 {
		t.Errorf("after delete: len=%d", len(srcs))
	}
}

func TestRegistry_TenantIsolation(t *testing.T) {
	store := &inMemSourceStore{}
	if !registered("stub4") {
		Register("stub4", (&stubFactory{name: "stub4"}).Build)
	}
	cfgA, _ := json.Marshal(map[string]string{})
	store.rows = append(store.rows,
		&ifaces.SkillSourceRecord{TenantID: "tenantA", Name: "secret", Driver: "stub4", ConfigJSON: cfgA, Priority: 100, Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	)
	reg := NewRegistry(store, nil, nil, t.TempDir(), discardLog(), nil)
	a, _ := reg.Sources(context.Background(), "tenantA")
	b, _ := reg.Sources(context.Background(), "tenantB")
	if len(a) != 1 || len(b) != 0 {
		t.Errorf("tenant isolation failed: A=%d B=%d", len(a), len(b))
	}
}

// --- helpers --------------------------------------------------------

func discardLog() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func srcNames(in []Source) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, s.Name())
	}
	return out
}

func registered(name string) bool {
	for _, n := range Drivers() {
		if n == name {
			return true
		}
	}
	return false
}
