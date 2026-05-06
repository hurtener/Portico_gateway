package mcpgw

import (
	"context"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/apps"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/registry"
)

func newAggregatorForTest() *ResourceAggregator {
	// Reuse fakeFleet (defined in resources_test.go) with no servers so
	// ListAll is well-defined; cache invalidation is what matters here.
	return NewResourceAggregator(&fakeFleet{servers: map[string]*registry.Snapshot{}, clients: map[string]*fakeSouthClient{}}, apps.New(apps.CSPConfig{}), ResourceLimits{}, discardLogger())
}

func TestListChanged_StableMode_SuppressesAndInvalidates(t *testing.T) {
	sessions := NewSessionRegistry()
	sess := sessions.Create("acme", "u1")
	agg := newAggregatorForTest()
	// Prime the cache so we can detect invalidation.
	agg.storeCache(sess.ID, "resources", "", []byte(`{"resources":[]}`))

	mux := NewListChangedMux(sessions, agg, ModeStable, discardLogger())
	mux.Subscribe(sess.ID, "github")

	mux.OnDownstream(context.Background(), "github", protocol.Notification{
		Method: protocol.NotifResourcesListChanged,
	})

	// Stable mode → cache invalidated, no notification on the session.
	if _, ok := agg.lookupCache(sess.ID, "resources", ""); ok {
		t.Errorf("cache should be invalidated in stable mode")
	}

	select {
	case n := <-sess.Notifications():
		t.Errorf("unexpected forwarded notification in stable mode: %+v", n)
	case <-time.After(50 * time.Millisecond):
		// expected: nothing
	}
}

func TestListChanged_LiveMode_Forwards(t *testing.T) {
	sessions := NewSessionRegistry()
	sess := sessions.Create("acme", "u1")
	agg := newAggregatorForTest()
	mux := NewListChangedMux(sessions, agg, ModeStable, discardLogger())
	mux.SetMode(sess.ID, ModeLive)
	mux.Subscribe(sess.ID, "github")

	mux.OnDownstream(context.Background(), "github", protocol.Notification{
		Method: protocol.NotifResourcesListChanged,
	})

	select {
	case n := <-sess.Notifications():
		if n.Method != protocol.NotifResourcesListChanged {
			t.Errorf("forwarded method = %q", n.Method)
		}
	case <-time.After(time.Second):
		t.Errorf("live-mode forward never arrived")
	}
}

func TestListChanged_MixedSessions_DifferentModes(t *testing.T) {
	sessions := NewSessionRegistry()
	live := sessions.Create("acme", "u1")
	stable := sessions.Create("acme", "u2")
	agg := newAggregatorForTest()
	mux := NewListChangedMux(sessions, agg, ModeStable, discardLogger())
	mux.SetMode(live.ID, ModeLive)
	mux.Subscribe(live.ID, "github")
	mux.Subscribe(stable.ID, "github")

	mux.OnDownstream(context.Background(), "github", protocol.Notification{Method: protocol.NotifResourcesListChanged})

	select {
	case <-live.Notifications():
		// expected
	case <-time.After(time.Second):
		t.Errorf("live session never received forward")
	}

	select {
	case n := <-stable.Notifications():
		t.Errorf("stable session should not receive: %+v", n)
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestListChanged_ForgetSessionDropsState(t *testing.T) {
	sessions := NewSessionRegistry()
	sess := sessions.Create("acme", "u")
	mux := NewListChangedMux(sessions, newAggregatorForTest(), ModeStable, discardLogger())
	mux.SetMode(sess.ID, ModeLive)
	mux.Subscribe(sess.ID, "github")
	mux.ForgetSession(sess.ID)
	if mux.Mode(sess.ID) != ModeStable {
		t.Errorf("Mode should fall back to default after Forget")
	}
}
