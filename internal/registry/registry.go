package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// ChangeKind classifies a registry mutation surfaced to subscribers.
type ChangeKind int

const (
	ChangeAdded ChangeKind = iota + 1
	ChangeUpdated
	ChangeRemoved
)

func (c ChangeKind) String() string {
	switch c {
	case ChangeAdded:
		return "added"
	case ChangeUpdated:
		return "updated"
	case ChangeRemoved:
		return "removed"
	default:
		return "unknown"
	}
}

// ChangeEvent is broadcast to every subscriber whenever the registry
// mutates. Subscribers must drain promptly; the registry buffers a small
// window per subscriber and drops oldest on overflow.
type ChangeEvent struct {
	Kind     ChangeKind
	TenantID string
	ServerID string
	Old      *Snapshot
	New      *Snapshot
}

// Snapshot is the immutable post-merge view of a server, ready for
// supervisor consumption. Spec is the canonical effective spec; Record is
// the storage row (status, schema_hash, etc).
type Snapshot struct {
	Spec   ServerSpec
	Record ifaces.ServerRecord
}

// Registry exposes tenant-scoped CRUD over the persistent store and
// publishes change events for the supervisor.
type Registry struct {
	store ifaces.RegistryStore
	log   *slog.Logger

	subMu       sync.RWMutex
	subscribers map[chan ChangeEvent]struct{}
}

// New builds a Registry over the supplied store.
func New(store ifaces.RegistryStore, log *slog.Logger) *Registry {
	if log == nil {
		log = slog.Default()
	}
	return &Registry{
		store:       store,
		log:         log,
		subscribers: make(map[chan ChangeEvent]struct{}),
	}
}

// Get returns the snapshot for (tenant, server). Returns ifaces.ErrNotFound
// when the row is absent.
func (r *Registry) Get(ctx context.Context, tenantID, id string) (*Snapshot, error) {
	rec, err := r.store.GetServer(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return recordToSnapshot(rec)
}

// List returns every server registered for the tenant.
func (r *Registry) List(ctx context.Context, tenantID string) ([]*Snapshot, error) {
	recs, err := r.store.ListServers(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]*Snapshot, 0, len(recs))
	for _, rec := range recs {
		s, err := recordToSnapshot(rec)
		if err != nil {
			r.log.Warn("registry: skipping malformed record",
				"tenant_id", rec.TenantID, "server_id", rec.ID, "err", err)
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// Upsert validates the spec, persists it, and broadcasts a ChangeEvent.
// Returns the materialized snapshot.
func (r *Registry) Upsert(ctx context.Context, tenantID string, spec *ServerSpec) (*Snapshot, error) {
	if tenantID == "" {
		return nil, errors.New("registry: tenant id is required")
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	specBytes, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("registry: marshal spec: %w", err)
	}

	old, _ := r.store.GetServer(ctx, tenantID, spec.ID)

	enabled := true
	if spec.Enabled != nil {
		enabled = *spec.Enabled
	}
	rec := &ifaces.ServerRecord{
		TenantID:    tenantID,
		ID:          spec.ID,
		DisplayName: spec.DisplayName,
		Transport:   spec.Transport,
		RuntimeMode: spec.RuntimeMode,
		Spec:        specBytes,
		Enabled:     enabled,
		Status:      StatusUnknown,
	}
	if old != nil {
		rec.CreatedAt = old.CreatedAt
		rec.Status = old.Status
		rec.StatusDetail = old.StatusDetail
		rec.SchemaHash = old.SchemaHash
		rec.LastError = old.LastError
	}
	rec.UpdatedAt = time.Now().UTC()
	if err := r.store.UpsertServer(ctx, rec); err != nil {
		return nil, err
	}
	snap, err := recordToSnapshot(rec)
	if err != nil {
		return nil, err
	}
	kind := ChangeUpdated
	var oldSnap *Snapshot
	if old == nil {
		kind = ChangeAdded
	} else {
		oldSnap, _ = recordToSnapshot(old)
	}
	r.publish(ChangeEvent{Kind: kind, TenantID: tenantID, ServerID: spec.ID, Old: oldSnap, New: snap})
	return snap, nil
}

// Delete removes the server and broadcasts a ChangeRemoved event.
func (r *Registry) Delete(ctx context.Context, tenantID, id string) error {
	if tenantID == "" || id == "" {
		return errors.New("registry: tenant_id and id are required")
	}
	old, err := r.store.GetServer(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if err := r.store.DeleteServer(ctx, tenantID, id); err != nil {
		return err
	}
	oldSnap, _ := recordToSnapshot(old)
	r.publish(ChangeEvent{Kind: ChangeRemoved, TenantID: tenantID, ServerID: id, Old: oldSnap})
	return nil
}

// SetEnabled toggles enabled without changing the spec.
func (r *Registry) SetEnabled(ctx context.Context, tenantID, id string, enabled bool) (*Snapshot, error) {
	rec, err := r.store.GetServer(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if rec.Enabled == enabled {
		return recordToSnapshot(rec)
	}
	rec.Enabled = enabled
	if !enabled {
		rec.Status = StatusDisabled
	} else if rec.Status == StatusDisabled {
		rec.Status = StatusUnknown
	}
	rec.UpdatedAt = time.Now().UTC()
	if err := r.store.UpsertServer(ctx, rec); err != nil {
		return nil, err
	}
	snap, err := recordToSnapshot(rec)
	if err != nil {
		return nil, err
	}
	r.publish(ChangeEvent{Kind: ChangeUpdated, TenantID: tenantID, ServerID: id, New: snap})
	return snap, nil
}

// UpdateStatus persists the supervisor's view of the server's health.
func (r *Registry) UpdateStatus(ctx context.Context, tenantID, id, status, detail string) error {
	return r.store.UpdateServerStatus(ctx, tenantID, id, status, detail)
}

// ListInstances returns the supervisor's bookkeeping rows for a server.
func (r *Registry) ListInstances(ctx context.Context, tenantID, serverID string) ([]*ifaces.InstanceRecord, error) {
	return r.store.ListInstances(ctx, tenantID, serverID)
}

// UpsertInstance / DeleteInstance are wrappers for supervisor-side use.
func (r *Registry) UpsertInstance(ctx context.Context, i *ifaces.InstanceRecord) error {
	return r.store.UpsertInstance(ctx, i)
}

func (r *Registry) DeleteInstance(ctx context.Context, tenantID, id string) error {
	return r.store.DeleteInstance(ctx, tenantID, id)
}

// Subscribe returns a channel that receives every mutation. Buffered (32);
// drops oldest on overflow. Unsubscribe must close the channel via
// Unsubscribe to stop the publisher from holding it.
func (r *Registry) Subscribe() <-chan ChangeEvent {
	ch := make(chan ChangeEvent, 32)
	r.subMu.Lock()
	r.subscribers[ch] = struct{}{}
	r.subMu.Unlock()
	return ch
}

// Unsubscribe removes the subscriber and closes the channel.
func (r *Registry) Unsubscribe(ch <-chan ChangeEvent) {
	r.subMu.Lock()
	defer r.subMu.Unlock()
	for c := range r.subscribers {
		if c == ch {
			delete(r.subscribers, c)
			close(c)
			return
		}
	}
}

// CloseAll terminates every subscription. Used on server shutdown.
func (r *Registry) CloseAll() {
	r.subMu.Lock()
	defer r.subMu.Unlock()
	for c := range r.subscribers {
		close(c)
	}
	r.subscribers = make(map[chan ChangeEvent]struct{})
}

func (r *Registry) publish(ev ChangeEvent) {
	r.subMu.RLock()
	subs := make([]chan ChangeEvent, 0, len(r.subscribers))
	for c := range r.subscribers {
		subs = append(subs, c)
	}
	r.subMu.RUnlock()
	for _, c := range subs {
		select {
		case c <- ev:
		default:
			// Drop oldest, push newest.
			select {
			case <-c:
			default:
			}
			select {
			case c <- ev:
			default:
			}
		}
	}
}

// recordToSnapshot decodes the JSON spec stored in the row into a typed
// ServerSpec and pairs it with the record. Used by handlers + supervisor.
func recordToSnapshot(rec *ifaces.ServerRecord) (*Snapshot, error) {
	if rec == nil {
		return nil, ifaces.ErrNotFound
	}
	var spec ServerSpec
	if len(rec.Spec) > 0 {
		if err := json.Unmarshal(rec.Spec, &spec); err != nil {
			return nil, fmt.Errorf("registry: decode spec for %s/%s: %w", rec.TenantID, rec.ID, err)
		}
	}
	return &Snapshot{Spec: spec, Record: *rec}, nil
}
