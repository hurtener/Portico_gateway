package snapshots

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// LiveProbe is the seam the drift detector consults to recompute
// per-server tool fingerprints. The dispatcher's manager satisfies it in
// production; tests pass a fake.
type LiveProbe interface {
	// ListTools returns the *current* per-server tool list for tenantID,
	// keyed by server id.
	ListTools(ctx context.Context, tenantID string) (map[string][]protocol.Tool, error)
}

// Detector walks active sessions on a tick, re-fingerprints each server
// they reference, and emits schema.drift when a snapshot's recorded hash
// no longer matches live state. Cheap when no drift is found.
type Detector struct {
	service  *Service
	probe    LiveProbe
	emitter  audit.Emitter
	log      *slog.Logger
	interval time.Duration

	stopOnce sync.Once
	stop     chan struct{}
	wg       sync.WaitGroup
}

// NewDetector wires the drift detector. interval defaults to 60s.
func NewDetector(service *Service, probe LiveProbe, log *slog.Logger, interval time.Duration) *Detector {
	if log == nil {
		log = slog.Default()
	}
	if interval <= 0 {
		interval = 60 * time.Second
	}
	em := audit.Emitter(audit.NopEmitter{})
	if service != nil && service.EmitterFor() != nil {
		em = service.EmitterFor()
	}
	return &Detector{
		service:  service,
		probe:    probe,
		emitter:  em,
		log:      log,
		interval: interval,
		stop:     make(chan struct{}),
	}
}

// Start kicks off the detection loop. Returns immediately.
func (d *Detector) Start(ctx context.Context) {
	d.wg.Add(1)
	go d.run(ctx)
}

// Stop signals the loop to terminate and joins it. Idempotent.
func (d *Detector) Stop() {
	d.stopOnce.Do(func() {
		close(d.stop)
		d.wg.Wait()
	})
}

// Once runs a single drift sweep — useful for tests that don't want to
// time the goroutine.
func (d *Detector) Once(ctx context.Context) error {
	return d.sweep(ctx)
}

func (d *Detector) run(ctx context.Context) {
	defer d.wg.Done()
	t := time.NewTicker(d.interval)
	defer t.Stop()
	// Run the first sweep immediately so a freshly-booted gateway is
	// useful right away.
	_ = d.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stop:
			return
		case <-t.C:
			if err := d.sweep(ctx); err != nil {
				d.log.Warn("drift: sweep failed", "err", err)
			}
		}
	}
}

func (d *Detector) sweep(ctx context.Context) error {
	if d.service == nil || d.probe == nil {
		return nil
	}
	// Skip sessions older than 24h — assume abandoned.
	cutoff := time.Now().Add(-24 * time.Hour)
	store := d.service.StoreFor()
	active, err := store.ActiveSessions(ctx, cutoff)
	if err != nil {
		return err
	}
	// Group by (tenant, server) so we re-probe each pair at most once
	// per sweep.
	per := make(map[string]map[string][]protocol.Tool) // tenantID -> serverID -> tools
	tenants := make(map[string]struct{})
	for _, s := range active {
		tenants[s.TenantID] = struct{}{}
	}
	for tenantID := range tenants {
		got, err := d.probe.ListTools(ctx, tenantID)
		if err != nil {
			d.log.Warn("drift: list tools failed", "tenant_id", tenantID, "err", err)
			continue
		}
		per[tenantID] = got
	}
	for _, sess := range active {
		if sess.SnapshotID == "" {
			continue
		}
		snap, err := store.Get(ctx, sess.SnapshotID)
		if err != nil {
			continue
		}
		serverTools := per[sess.TenantID]
		for _, sv := range snap.Servers {
			tools := serverTools[sv.ID]
			currentHash := ServerToolsFingerprint(tools)
			if currentHash == sv.SchemaHash {
				continue
			}
			oldRefs := indexToolsByName(snap.Tools)
			newRefs := indexNewTools(tools, sv.ID)
			diff := diffTools(oldRefs, newRefs, sv.ID)
			d.emitter.Emit(ctx, audit.Event{
				Type:      "schema.drift",
				TenantID:  sess.TenantID,
				SessionID: sess.SessionID,
				Payload: map[string]any{
					"snapshot_id": snap.ID,
					"server_id":   sv.ID,
					"old_hash":    sv.SchemaHash,
					"new_hash":    currentHash,
					"diff":        map[string]any{"tools": diff},
				},
			})
			_ = store.UpsertFingerprint(ctx, sess.TenantID, sv.ID, currentHash, len(tools))
		}
	}
	return nil
}

func indexNewTools(tools []protocol.Tool, serverID string) map[string]ToolInfo {
	out := make(map[string]ToolInfo, len(tools))
	for _, t := range tools {
		ti := ToolInfo{
			NamespacedName: serverID + "." + t.Name,
			ServerID:       serverID,
			Description:    t.Description,
			InputSchema:    t.InputSchema,
			Annotations:    t.Annotations,
		}
		ti.Hash = ToolFingerprint(ti)
		out[ti.NamespacedName] = ti
	}
	return out
}

func diffTools(oldRefs, newRefs map[string]ToolInfo, serverID string) ToolDiff {
	d := ToolDiff{}
	for name, nt := range newRefs {
		if _, ok := oldRefs[name]; !ok {
			d.Added = append(d.Added, name)
			continue
		}
		_ = nt
	}
	for name, ot := range oldRefs {
		if ot.ServerID != serverID {
			continue
		}
		nt, ok := newRefs[name]
		if !ok {
			d.Removed = append(d.Removed, name)
			continue
		}
		if ot.Hash != nt.Hash {
			d.Modified = append(d.Modified, ModifiedTool{
				Name:          name,
				FieldsChanged: changedFields(ot, nt),
				OldHash:       ot.Hash,
				NewHash:       nt.Hash,
			})
		}
	}
	return d
}
