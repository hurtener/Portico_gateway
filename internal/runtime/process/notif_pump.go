package process

import (
	"context"
	"log/slog"
	"sync"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/mcp/southbound"
)

// NotifSink receives downstream notifications. The supervisor invokes
// the sink synchronously from the per-instance pump goroutine — sinks
// must not block. List-changed mux fans out further.
type NotifSink func(ctx context.Context, serverID string, n protocol.Notification)

// NotifPump fans every supervised client's notifications channel out to
// a single NotifSink. One pump per Supervisor; goroutines are managed
// per instance and join cleanly on Stop / instance close.
type NotifPump struct {
	log  *slog.Logger
	sink NotifSink

	mu       sync.Mutex
	stops    map[string]context.CancelFunc // instance id -> cancel
	stopped  bool
	finished sync.WaitGroup
}

// NewNotifPump constructs a pump with the supplied sink. A nil sink
// disables forwarding (Track becomes a no-op).
func NewNotifPump(log *slog.Logger, sink NotifSink) *NotifPump {
	if log == nil {
		log = slog.Default()
	}
	return &NotifPump{log: log, sink: sink, stops: make(map[string]context.CancelFunc)}
}

// SetSink swaps the sink. Used during construction order — the sink
// (list-changed mux) is built after the pump.
func (p *NotifPump) SetSink(sink NotifSink) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.sink = sink
	p.mu.Unlock()
}

// Track starts a forwarding goroutine for a freshly-running instance.
// Called by the supervisor after a successful start; idempotent.
func (p *NotifPump) Track(inst *instance) {
	if p == nil || p.sink == nil {
		return
	}
	inst.mu.Lock()
	client := inst.client
	inst.mu.Unlock()
	if client == nil {
		return
	}

	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	if _, exists := p.stops[inst.id]; exists {
		p.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.stops[inst.id] = cancel
	p.finished.Add(1)
	p.mu.Unlock()

	go p.run(ctx, inst, client)
}

// Untrack stops a per-instance forwarder. Idempotent.
func (p *NotifPump) Untrack(instID string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	cancel, ok := p.stops[instID]
	delete(p.stops, instID)
	p.mu.Unlock()
	if ok {
		cancel()
	}
}

// Stop tears down every forwarder; safe to call once at supervisor
// shutdown. Joins all goroutines.
func (p *NotifPump) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.stopped = true
	for _, cancel := range p.stops {
		cancel()
	}
	p.stops = nil
	p.mu.Unlock()
	p.finished.Wait()
}

func (p *NotifPump) run(ctx context.Context, inst *instance, client southbound.Client) {
	defer p.finished.Done()
	ch := client.Notifications()
	if ch == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case n, ok := <-ch:
			if !ok {
				return
			}
			p.deliver(ctx, inst.spec.ID, n)
		}
	}
}

func (p *NotifPump) deliver(ctx context.Context, serverID string, n protocol.Notification) {
	p.mu.Lock()
	sink := p.sink
	p.mu.Unlock()
	if sink == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			p.log.Warn("notif sink panic recovered",
				"server_id", serverID,
				"method", n.Method,
				"panic", r)
		}
	}()
	sink(ctx, serverID, n)
}
