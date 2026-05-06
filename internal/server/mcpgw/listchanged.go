package mcpgw

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// ListChangedMode controls whether downstream list_changed notifications
// reach the northbound client.
//
//   - ModeStable (default) drops them at the mux and invalidates the
//     aggregator's cache; clients see fresh data on their next list call.
//   - ModeLive forwards the notification immediately so connected
//     clients can react in real time.
type ListChangedMode string

const (
	ModeStable ListChangedMode = "stable"
	ModeLive   ListChangedMode = "live"
)

// ListChangedMux dispatches downstream notifications to sessions
// according to the per-session mode + cache invalidation rules.
type ListChangedMux struct {
	log       *slog.Logger
	sessions  *SessionRegistry
	resources *ResourceAggregator

	mu          sync.RWMutex
	modes       map[string]ListChangedMode // sessionID -> mode
	defaultMode ListChangedMode

	// subscriptions: serverID -> set of sessionIDs that should receive
	// notifications from that server.
	subs map[string]map[string]struct{}
}

// NewListChangedMux constructs a mux. defaultMode controls behavior for
// sessions that have not opted in via initialize capabilities.
func NewListChangedMux(sessions *SessionRegistry, agg *ResourceAggregator, defaultMode ListChangedMode, log *slog.Logger) *ListChangedMux {
	if log == nil {
		log = slog.Default()
	}
	if defaultMode != ModeLive {
		defaultMode = ModeStable
	}
	return &ListChangedMux{
		log:         log,
		sessions:    sessions,
		resources:   agg,
		modes:       make(map[string]ListChangedMode),
		subs:        make(map[string]map[string]struct{}),
		defaultMode: defaultMode,
	}
}

// SetMode records the mode chosen at initialize time. Called from the
// dispatcher's initialize handler when the client opts into live updates
// via `experimental.portico.listChanged`.
func (m *ListChangedMux) SetMode(sessionID string, mode ListChangedMode) {
	if sessionID == "" {
		return
	}
	if mode != ModeLive && mode != ModeStable {
		mode = ModeStable
	}
	m.mu.Lock()
	m.modes[sessionID] = mode
	m.mu.Unlock()
}

// Mode returns the effective mode for a session, defaulting when no
// explicit mode has been set.
func (m *ListChangedMux) Mode(sessionID string) ListChangedMode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if mode, ok := m.modes[sessionID]; ok {
		return mode
	}
	return m.defaultMode
}

// Subscribe records that sessionID should receive notifications from
// serverID. Called when the dispatcher routes a list call (the session
// implicitly cares about that server's catalog).
func (m *ListChangedMux) Subscribe(sessionID, serverID string) {
	if sessionID == "" || serverID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	set, ok := m.subs[serverID]
	if !ok {
		set = make(map[string]struct{})
		m.subs[serverID] = set
	}
	set[sessionID] = struct{}{}
}

// ForgetSession removes a session from every subscription. Called from
// SessionRegistry.Close paths to keep the maps from leaking.
func (m *ListChangedMux) ForgetSession(sessionID string) {
	if sessionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.modes, sessionID)
	for serverID, set := range m.subs {
		delete(set, sessionID)
		if len(set) == 0 {
			delete(m.subs, serverID)
		}
	}
}

// OnDownstream is called by every southbound client's notifications
// goroutine. The mux classifies the notification + dispatches per
// session mode.
func (m *ListChangedMux) OnDownstream(_ context.Context, serverID string, n protocol.Notification) {
	switch n.Method {
	case protocol.NotifResourcesListChanged,
		protocol.NotifPromptsListChanged,
		protocol.NotifToolsListChanged,
		protocol.NotifResourcesUpdated:
		m.dispatch(serverID, n)
	default:
		// Phase 3 only handles list-changed and resources/updated. Other
		// notifications are ignored.
		m.log.Debug("downstream notification ignored",
			"server_id", serverID, "method", n.Method)
	}
}

func (m *ListChangedMux) dispatch(serverID string, n protocol.Notification) {
	m.mu.RLock()
	subs := make([]string, 0, len(m.subs[serverID]))
	for sid := range m.subs[serverID] {
		subs = append(subs, sid)
	}
	m.mu.RUnlock()

	for _, sid := range subs {
		mode := m.Mode(sid)
		if mode == ModeStable {
			m.log.Info("list_changed suppressed",
				"event_type", "list_changed_suppressed",
				"session_id", sid,
				"server_id", serverID,
				"method", n.Method)
			if m.resources != nil {
				m.resources.InvalidateSession(sid)
			}
			continue
		}
		// Live: forward to the session's notification channel after
		// rewriting params to include the originating server id (so
		// clients can correlate without leaking upstream URIs).
		sess, ok := m.sessions.Get(sid)
		if !ok {
			continue
		}
		out := m.rewriteForClient(serverID, n)
		dropped := sess.EmitNotification(out)
		m.log.Info("list_changed forwarded",
			"event_type", "list_changed_forwarded",
			"session_id", sid,
			"server_id", serverID,
			"method", n.Method,
			"dropped", dropped)
	}
}

// rewriteForClient annotates the notification's params with the
// originating server id under `_meta.portico.serverID`. The aggregator
// caches are invalidated regardless of mode so the next list call
// returns the latest state.
func (m *ListChangedMux) rewriteForClient(serverID string, n protocol.Notification) protocol.Notification {
	root := map[string]any{}
	if len(n.Params) > 0 {
		if err := json.Unmarshal(n.Params, &root); err != nil {
			m.log.Warn("malformed downstream notification params; falling back to empty",
				"server_id", serverID, "method", n.Method, "err", err)
			root = map[string]any{}
		}
	}
	meta, _ := root["_meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	portico, _ := meta["portico"].(map[string]any)
	if portico == nil {
		portico = map[string]any{}
	}
	portico["serverID"] = serverID
	meta["portico"] = portico
	root["_meta"] = meta
	body, _ := json.Marshal(root)
	return protocol.Notification{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  n.Method,
		Params:  body,
	}
}
