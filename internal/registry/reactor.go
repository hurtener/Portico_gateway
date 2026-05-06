package registry

import (
	"context"
	"log/slog"
	"sync"
)

// Reactor consumes a Registry's change-event channel and dispatches each
// event to a Reactive consumer (typically the process supervisor).
//
// Lives in the registry package so the consumer doesn't have to manage
// the goroutine lifecycle and channel cleanup itself. Phase 2 wires the
// supervisor as the only consumer; Phase 6 will plug catalog snapshot
// invalidation in here as well.
type Reactor struct {
	reg *Registry
	r   Reactive
	log *slog.Logger

	once sync.Once
	stop chan struct{}
	done chan struct{}
}

// Reactive is the surface the supervisor (and other consumers) implement.
// Methods receive the snapshot data via copy; they must not retain
// pointers across calls.
type Reactive interface {
	OnAdded(ctx context.Context, snap *Snapshot)
	OnUpdated(ctx context.Context, oldSnap, newSnap *Snapshot)
	OnRemoved(ctx context.Context, snap *Snapshot)
}

// NewReactor builds (and starts) a Reactor.
func NewReactor(reg *Registry, r Reactive, log *slog.Logger) *Reactor {
	if log == nil {
		log = slog.Default()
	}
	out := &Reactor{
		reg:  reg,
		r:    r,
		log:  log,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go out.loop()
	return out
}

// Stop halts the consumer and waits for the goroutine to exit.
func (r *Reactor) Stop() {
	r.once.Do(func() { close(r.stop) })
	<-r.done
}

func (r *Reactor) loop() {
	defer close(r.done)
	events := r.reg.Subscribe()
	defer r.reg.Unsubscribe(events)
	ctx := context.Background()
	for {
		select {
		case <-r.stop:
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			r.dispatch(ctx, ev)
		}
	}
}

func (r *Reactor) dispatch(ctx context.Context, ev ChangeEvent) {
	defer func() {
		// A misbehaving consumer panic must not kill the goroutine.
		if rec := recover(); rec != nil {
			r.log.Error("registry reactor: consumer panic",
				"tenant_id", ev.TenantID, "server_id", ev.ServerID, "kind", ev.Kind, "panic", rec)
		}
	}()
	switch ev.Kind {
	case ChangeAdded:
		r.r.OnAdded(ctx, ev.New)
	case ChangeUpdated:
		r.r.OnUpdated(ctx, ev.Old, ev.New)
	case ChangeRemoved:
		r.r.OnRemoved(ctx, ev.Old)
	}
}
