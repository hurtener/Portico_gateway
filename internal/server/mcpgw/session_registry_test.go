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
