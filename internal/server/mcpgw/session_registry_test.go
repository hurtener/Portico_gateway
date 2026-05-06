package mcpgw

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// TestSession_EmitNotification_NoRaceWithClose hammers EmitNotification and
// Close in parallel. Without the notifMu protection a concurrent close + send
// panics on the closed channel.
func TestSession_EmitNotification_NoRaceWithClose(t *testing.T) {
	s := newSession("s_test", "tenant-a", "user-1")
	body, _ := json.Marshal(protocol.ProgressParams{Progress: 1})
	n := protocol.Notification{JSONRPC: protocol.JSONRPCVersion, Method: protocol.NotifProgress, Params: body}

	// Drain notifications to avoid filling the channel.
	go func() {
		for range s.Notifications() { //nolint:revive // empty-block: only draining
		}
	}()

	var wg sync.WaitGroup
	const emitters = 16
	const perEmitter = 200
	wg.Add(emitters)
	for i := 0; i < emitters; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perEmitter; j++ {
				s.EmitNotification(n)
			}
		}()
	}

	// Close racing with the emitters.
	go func() {
		time.Sleep(2 * time.Millisecond)
		s.Close()
	}()

	wg.Wait()
}

// TestSession_EmitNotification_AfterClose returns dropped=true and never panics.
func TestSession_EmitNotification_AfterClose(t *testing.T) {
	s := newSession("s_test", "tenant-a", "user-1")
	s.Close()
	body, _ := json.Marshal(protocol.ProgressParams{Progress: 1})
	n := protocol.Notification{JSONRPC: protocol.JSONRPCVersion, Method: protocol.NotifProgress, Params: body}
	if dropped := s.EmitNotification(n); !dropped {
		t.Errorf("expected dropped=true after Close")
	}
}

// TestSessionRegistry_OnCloseFiresOnExplicitCloseAndCloseAll verifies the
// OnClose hook is invoked for both single-session Close and bulk
// CloseAll paths. The dispatcher relies on these hooks to drop
// per-session caches.
func TestSessionRegistry_OnCloseFiresOnExplicitCloseAndCloseAll(t *testing.T) {
	r := NewSessionRegistry()
	var mu sync.Mutex
	closed := map[string]int{}
	r.OnClose(func(id string) {
		mu.Lock()
		defer mu.Unlock()
		closed[id]++
	})

	a := r.Create("acme", "u1")
	b := r.Create("acme", "u2")
	r.Close(a.ID)
	r.CloseAll()

	mu.Lock()
	defer mu.Unlock()
	if closed[a.ID] != 1 {
		t.Errorf("session %s: OnClose fired %d times; want 1", a.ID, closed[a.ID])
	}
	if closed[b.ID] != 1 {
		t.Errorf("session %s: OnClose fired %d times; want 1", b.ID, closed[b.ID])
	}
}
