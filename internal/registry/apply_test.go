package registry_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// memStore is a tiny in-memory RegistryStore for Apply unit tests. It lets
// us exercise Apply without booting a SQLite DB.
type memStore struct {
	servers   map[string]*ifaces.ServerRecord
	instances map[string][]*ifaces.InstanceRecord
}

func newMemStore() *memStore {
	return &memStore{servers: map[string]*ifaces.ServerRecord{}, instances: map[string][]*ifaces.InstanceRecord{}}
}

func key(tenantID, id string) string { return tenantID + "/" + id }

func (m *memStore) UpsertServer(_ context.Context, r *ifaces.ServerRecord) error {
	cp := *r
	m.servers[key(r.TenantID, r.ID)] = &cp
	return nil
}
func (m *memStore) GetServer(_ context.Context, tenantID, id string) (*ifaces.ServerRecord, error) {
	r, ok := m.servers[key(tenantID, id)]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	cp := *r
	return &cp, nil
}
func (m *memStore) ListServers(_ context.Context, tenantID string) ([]*ifaces.ServerRecord, error) {
	var out []*ifaces.ServerRecord
	for k, v := range m.servers {
		if v.TenantID == tenantID {
			cp := *v
			_ = k
			out = append(out, &cp)
		}
	}
	return out, nil
}
func (m *memStore) DeleteServer(_ context.Context, tenantID, id string) error {
	if _, ok := m.servers[key(tenantID, id)]; !ok {
		return ifaces.ErrNotFound
	}
	delete(m.servers, key(tenantID, id))
	return nil
}
func (m *memStore) UpdateServerStatus(_ context.Context, tenantID, id, status, detail string) error {
	r, ok := m.servers[key(tenantID, id)]
	if !ok {
		return ifaces.ErrNotFound
	}
	r.Status = status
	r.StatusDetail = detail
	return nil
}
func (m *memStore) UpsertInstance(_ context.Context, _ *ifaces.InstanceRecord) error { return nil }
func (m *memStore) DeleteInstance(_ context.Context, _, _ string) error              { return nil }
func (m *memStore) ListInstances(_ context.Context, _, _ string) ([]*ifaces.InstanceRecord, error) {
	return nil, nil
}

func TestApply_Create_Inserts(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	enabled := true
	spec := &registry.ServerSpec{ID: "srv1", Transport: "stdio", Stdio: &registry.StdioSpec{Command: "/bin/true"}, Enabled: &enabled}
	snap, err := r.Apply(context.Background(), "acme", registry.Mutation{Op: registry.MutOpCreate, Server: spec})
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil || snap.Spec.ID != "srv1" {
		t.Errorf("create returned %+v", snap)
	}
	// Re-creating should fail.
	if _, err := r.Apply(context.Background(), "acme", registry.Mutation{Op: registry.MutOpCreate, Server: spec}); err == nil {
		t.Errorf("expected error on duplicate create")
	}
}

func TestApply_Update_Idempotent(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	enabled := true
	spec := &registry.ServerSpec{ID: "srv1", Transport: "stdio", Stdio: &registry.StdioSpec{Command: "/bin/true"}, Enabled: &enabled}
	if _, err := r.Apply(context.Background(), "acme", registry.Mutation{Op: registry.MutOpCreate, Server: spec}); err != nil {
		t.Fatal(err)
	}
	spec.DisplayName = "updated"
	if _, err := r.Apply(context.Background(), "acme", registry.Mutation{Op: registry.MutOpUpdate, Server: spec}); err != nil {
		t.Fatal(err)
	}
	got, err := r.Get(context.Background(), "acme", "srv1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec.DisplayName != "updated" {
		t.Errorf("update did not apply: %+v", got.Spec)
	}
}

func TestApply_Delete(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	enabled := true
	spec := &registry.ServerSpec{ID: "srv1", Transport: "stdio", Stdio: &registry.StdioSpec{Command: "/bin/true"}, Enabled: &enabled}
	if _, err := r.Apply(context.Background(), "acme", registry.Mutation{Op: registry.MutOpCreate, Server: spec}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Apply(context.Background(), "acme", registry.Mutation{Op: registry.MutOpDelete, ServerID: "srv1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(context.Background(), "acme", "srv1"); !errors.Is(err, ifaces.ErrNotFound) {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestApply_RejectsInvalidConfig(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	if _, err := r.Apply(context.Background(), "acme", registry.Mutation{Op: registry.MutOpCreate, Server: &registry.ServerSpec{}}); err == nil {
		t.Errorf("expected validation error on empty spec")
	}
}

func TestRestart_PublishesEvent(t *testing.T) {
	r := registry.New(newMemStore(), nil)
	enabled := true
	spec := &registry.ServerSpec{ID: "srv1", Transport: "stdio", Stdio: &registry.StdioSpec{Command: "/bin/true"}, Enabled: &enabled}
	if _, err := r.Apply(context.Background(), "acme", registry.Mutation{Op: registry.MutOpCreate, Server: spec}); err != nil {
		t.Fatal(err)
	}
	sub := r.Subscribe()
	defer r.Unsubscribe(sub)
	go func() {
		_, _ = r.Restart(context.Background(), "acme", "srv1", "test")
	}()
	select {
	case ev := <-sub:
		if ev.Kind != registry.ChangeUpdated || ev.ServerID != "srv1" {
			t.Errorf("unexpected event: %+v", ev)
		}
	case <-context.Background().Done():
		t.Fatal("no event")
	}
}
