package dispatch_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/a2a/dispatch"
	a2a "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
	"github.com/hurtener/Portico_gateway/internal/a2a/southbound"
	"github.com/hurtener/Portico_gateway/internal/a2a/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/profiles"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// --- fakes -----------------------------------------------------------------

type fakeStore struct {
	mu    sync.Mutex
	peers map[string]*ifaces.A2APeer
}

func newFakeStore(peers ...*ifaces.A2APeer) *fakeStore {
	s := &fakeStore{peers: map[string]*ifaces.A2APeer{}}
	for _, p := range peers {
		s.peers[p.TenantID+"/"+p.ID] = p
	}
	return s
}

func (s *fakeStore) PutPeer(_ context.Context, p *ifaces.A2APeer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers[p.TenantID+"/"+p.ID] = p
	return nil
}

func (s *fakeStore) GetPeer(_ context.Context, tenantID, id string) (*ifaces.A2APeer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.peers[tenantID+"/"+id]
	if !ok {
		return nil, ifaces.ErrA2APeerNotFound
	}
	cp := *p
	return &cp, nil
}

func (s *fakeStore) ListPeers(_ context.Context, tenantID string) ([]*ifaces.A2APeer, error) {
	return nil, nil
}

func (s *fakeStore) DeletePeer(_ context.Context, tenantID, id string) error { return nil }

// stubClient returns canned responses (or a canned error) for each method.
type stubClient struct {
	sendResult json.RawMessage
	task       *a2a.Task
	err        error
}

func (c *stubClient) FetchAgentCard(context.Context, string) (*a2a.AgentCard, error) { return nil, nil }
func (c *stubClient) SendMessage(context.Context, a2a.MessageSendParams) (json.RawMessage, error) {
	return c.sendResult, c.err
}
func (c *stubClient) GetTask(context.Context, a2a.TaskQueryParams) (*a2a.Task, error) {
	return c.task, c.err
}
func (c *stubClient) CancelTask(context.Context, a2a.TaskIDParams) (*a2a.Task, error) {
	return c.task, c.err
}
func (c *stubClient) Close(context.Context) error { return nil }

type recEmitter struct {
	mu     sync.Mutex
	events []audit.Event
}

func (e *recEmitter) Emit(_ context.Context, ev audit.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
}
func (e *recEmitter) types() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.events))
	for i, ev := range e.events {
		out[i] = ev.Type
	}
	return out
}

func hasType(types []string, want string) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

func enabledPeer(tenant, id, name string) *ifaces.A2APeer {
	return &ifaces.A2APeer{TenantID: tenant, ID: id, Name: name, Endpoint: "https://x/a2a", Enabled: true}
}

func newDispatcher(t *testing.T, client southbound.Client, store *fakeStore, em audit.Emitter) *dispatch.Dispatcher {
	t.Helper()
	factory := func(context.Context, *ifaces.A2APeer) (southbound.Client, error) { return client, nil }
	pool := manager.NewPool(store, factory, nil)
	return dispatch.New(store, pool, em, nil)
}

// --- tests -----------------------------------------------------------------

func TestSendMessage_AllowedByDefaultProfile(t *testing.T) {
	store := newFakeStore(enabledPeer("t1", "peer-1", "research-agent"))
	client := &stubClient{sendResult: json.RawMessage(`{"id":"task-1","kind":"task"}`)}
	em := &recEmitter{}
	d := newDispatcher(t, client, store, em)

	ctx := profiles.WithProfile(context.Background(), profiles.DefaultProfile("t1"))
	res, aerr := d.SendMessage(ctx, "t1", "peer-1", a2a.MessageSendParams{})
	if aerr != nil {
		t.Fatalf("unexpected error: %+v", aerr)
	}
	if string(res) != `{"id":"task-1","kind":"task"}` {
		t.Errorf("result = %s", res)
	}
	if !hasType(em.types(), audit.EventA2ADispatch) {
		t.Errorf("expected a2a.dispatch audit event, got %v", em.types())
	}
}

func TestSendMessage_DeniedByProfile(t *testing.T) {
	store := newFakeStore(enabledPeer("t1", "peer-1", "research-agent"))
	client := &stubClient{sendResult: json.RawMessage(`{}`)}
	em := &recEmitter{}
	d := newDispatcher(t, client, store, em)

	// Restrictive profile that does NOT list "research-agent".
	prof := &profiles.Profile{TenantID: "t1", ID: "ap_1", AllowedA2APeers: []string{"other-agent"}}
	ctx := profiles.WithProfile(context.Background(), prof)
	_, aerr := d.SendMessage(ctx, "t1", "peer-1", a2a.MessageSendParams{})
	if aerr == nil || aerr.Code != a2a.ErrProfileViolation {
		t.Fatalf("want ErrProfileViolation, got %+v", aerr)
	}
	if !hasType(em.types(), audit.EventAgentProfileViolation) {
		t.Errorf("expected agent_profile.violation audit event, got %v", em.types())
	}
}

func TestSendMessage_AllowedByRestrictiveProfileListingPeer(t *testing.T) {
	store := newFakeStore(enabledPeer("t1", "peer-1", "research-agent"))
	client := &stubClient{sendResult: json.RawMessage(`{"ok":true}`)}
	d := newDispatcher(t, client, store, &recEmitter{})

	prof := &profiles.Profile{TenantID: "t1", ID: "ap_1", AllowedA2APeers: []string{"research-agent"}}
	ctx := profiles.WithProfile(context.Background(), prof)
	if _, aerr := d.SendMessage(ctx, "t1", "peer-1", a2a.MessageSendParams{}); aerr != nil {
		t.Fatalf("listed peer should be allowed, got %+v", aerr)
	}
}

func TestSendMessage_UnknownPeer(t *testing.T) {
	store := newFakeStore()
	d := newDispatcher(t, &stubClient{}, store, &recEmitter{})
	ctx := profiles.WithProfile(context.Background(), profiles.DefaultProfile("t1"))
	_, aerr := d.SendMessage(ctx, "t1", "ghost", a2a.MessageSendParams{})
	if aerr == nil || aerr.Code != a2a.ErrInvalidParams {
		t.Fatalf("want ErrInvalidParams for unknown peer, got %+v", aerr)
	}
}

func TestSendMessage_PeerProtocolErrorPassesThrough(t *testing.T) {
	store := newFakeStore(enabledPeer("t1", "peer-1", "research-agent"))
	client := &stubClient{err: a2a.NewError(a2a.ErrTaskNotFound, "no such task")}
	d := newDispatcher(t, client, store, &recEmitter{})
	ctx := profiles.WithProfile(context.Background(), profiles.DefaultProfile("t1"))
	_, aerr := d.SendMessage(ctx, "t1", "peer-1", a2a.MessageSendParams{})
	if aerr == nil || aerr.Code != a2a.ErrTaskNotFound {
		t.Fatalf("peer JSON-RPC error should pass through, got %+v", aerr)
	}
}

func TestGetTask_DisabledPeer(t *testing.T) {
	peer := enabledPeer("t1", "peer-1", "research-agent")
	peer.Enabled = false
	store := newFakeStore(peer)
	d := newDispatcher(t, &stubClient{}, store, &recEmitter{})
	ctx := profiles.WithProfile(context.Background(), profiles.DefaultProfile("t1"))
	// Disabled peer is rejected at pool.Acquire → ErrUnsupportedOperation.
	_, aerr := d.GetTask(ctx, "t1", "peer-1", a2a.TaskQueryParams{ID: "x"})
	if aerr == nil || aerr.Code != a2a.ErrUnsupportedOperation {
		t.Fatalf("want ErrUnsupportedOperation for disabled peer, got %+v", aerr)
	}
}

func TestGetTask_Success(t *testing.T) {
	store := newFakeStore(enabledPeer("t1", "peer-1", "research-agent"))
	client := &stubClient{task: &a2a.Task{ID: "task-9", Status: a2a.TaskStatus{State: a2a.TaskStateCompleted}}}
	em := &recEmitter{}
	d := newDispatcher(t, client, store, em)
	ctx := profiles.WithProfile(context.Background(), profiles.DefaultProfile("t1"))
	task, aerr := d.GetTask(ctx, "t1", "peer-1", a2a.TaskQueryParams{ID: "task-9"})
	if aerr != nil {
		t.Fatalf("unexpected error: %+v", aerr)
	}
	if task == nil || task.ID != "task-9" || task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("task = %+v", task)
	}
	if !hasType(em.types(), audit.EventA2ADispatch) {
		t.Errorf("expected a2a.dispatch audit, got %v", em.types())
	}
}

// nil profile in context (no profile bound) is treated as full-surface allow.
func TestSendMessage_NilProfileAllows(t *testing.T) {
	store := newFakeStore(enabledPeer("t1", "peer-1", "research-agent"))
	client := &stubClient{sendResult: json.RawMessage(`{}`)}
	d := newDispatcher(t, client, store, &recEmitter{})
	if _, aerr := d.SendMessage(context.Background(), "t1", "peer-1", a2a.MessageSendParams{}); aerr != nil {
		t.Fatalf("nil profile should allow, got %+v", aerr)
	}
}
