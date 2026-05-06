// Package mcpgw is the gateway control surface that ties northbound MCP
// transport, southbound clients, and the dispatcher together. Phase 1 ships
// session registry + tool aggregation + dispatch.
package mcpgw

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// Session is the per-connection state tracked by the gateway.
type Session struct {
	ID         string
	TenantID   string
	UserID     string
	// SubjectToken is the raw bearer JWT the client presented at session
	// creation. Phase 5's OAuth token-exchange strategy reuses it as the
	// RFC 8693 subject_token. Sensitive: never log, persist, or surface.
	SubjectToken string
	ClientCaps   protocol.ClientCapsRecord
	InitParams   protocol.InitializeParams
	CreatedAt    time.Time
	LastSeenAt   atomic.Int64 // unix nanos

	// notifCh is the outbound notification channel served to the long-lived
	// SSE GET /mcp stream (and in Phase 5 to the server-initiated request
	// path). Bounded; oldest dropped on overflow.
	//
	// notifMu guards the channel against the close-vs-send race: Close()
	// must not close the channel while a concurrent EmitNotification is
	// selecting on it. All sends happen under notifMu; Close() closes it
	// under the same lock.
	notifMu sync.Mutex
	notifCh chan protocol.Notification

	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc // northbound request id -> cancel
	closed   atomic.Bool
}

func newSession(id, tenantID, userID, subjectToken string) *Session {
	s := &Session{
		ID:           id,
		TenantID:     tenantID,
		UserID:       userID,
		SubjectToken: subjectToken,
		CreatedAt:    time.Now().UTC(),
		notifCh:      make(chan protocol.Notification, 256),
		cancels:      make(map[string]context.CancelFunc),
	}
	s.LastSeenAt.Store(time.Now().UnixNano())
	return s
}

// Touch updates the last-seen timestamp.
func (s *Session) Touch() {
	s.LastSeenAt.Store(time.Now().UnixNano())
}

// Notifications returns the outbound channel for SSE delivery. Closed when
// the session terminates.
func (s *Session) Notifications() <-chan protocol.Notification {
	return s.notifCh
}

// EmitNotification queues a notification for the session's SSE consumer.
// Drops oldest on backpressure and reports it to the caller for logging.
//
// Holds notifMu for the duration of the send so a concurrent Close() cannot
// close the channel mid-send. Close() acquires the same lock before closing.
func (s *Session) EmitNotification(n protocol.Notification) (dropped bool) {
	s.notifMu.Lock()
	defer s.notifMu.Unlock()
	if s.closed.Load() {
		return true
	}
	select {
	case s.notifCh <- n:
		return false
	default:
		// Drop oldest, then push.
		select {
		case <-s.notifCh:
		default:
		}
		select {
		case s.notifCh <- n:
		default:
		}
		return true
	}
}

// RegisterCancel records the cancel function for an in-flight northbound
// request id so a notifications/cancelled can stop it.
func (s *Session) RegisterCancel(id string, cancel context.CancelFunc) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	s.cancels[id] = cancel
}

// UnregisterCancel removes the entry once a request completes.
func (s *Session) UnregisterCancel(id string) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	delete(s.cancels, id)
}

// Cancel triggers the registered cancel for an id (no-op if unknown).
func (s *Session) Cancel(id string) bool {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	if fn, ok := s.cancels[id]; ok {
		fn()
		delete(s.cancels, id)
		return true
	}
	return false
}

// Close marks the session terminated and closes the notification channel.
// Idempotent. Acquires notifMu so it cannot race a concurrent
// EmitNotification.
func (s *Session) Close() {
	if s.closed.Swap(true) {
		return
	}
	s.cancelMu.Lock()
	for _, fn := range s.cancels {
		fn()
	}
	s.cancels = nil
	s.cancelMu.Unlock()
	s.notifMu.Lock()
	close(s.notifCh)
	s.notifMu.Unlock()
}

// SessionRegistry holds active sessions, keyed by their public session id.
type SessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*Session

	onCloseMu sync.Mutex
	onClose   []func(sessionID string)
}

// OnClose registers a callback fired (synchronously) whenever a session
// terminates. The list-changed mux uses it to drop per-session state.
func (r *SessionRegistry) OnClose(fn func(sessionID string)) {
	if fn == nil {
		return
	}
	r.onCloseMu.Lock()
	r.onClose = append(r.onClose, fn)
	r.onCloseMu.Unlock()
}

func (r *SessionRegistry) fireClose(sessionID string) {
	r.onCloseMu.Lock()
	cbs := append([]func(string){}, r.onClose...)
	r.onCloseMu.Unlock()
	for _, fn := range cbs {
		fn(sessionID)
	}
}

func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{sessions: make(map[string]*Session)}
}

// Create makes a new session and returns it. Generates a cryptographically
// random session id. subjectToken is the raw bearer JWT (or "" in dev
// mode) — Phase 5's OAuth strategy reuses it as the RFC 8693 subject_token.
func (r *SessionRegistry) Create(tenantID, userID, subjectToken string) *Session {
	id := newSessionID()
	s := newSession(id, tenantID, userID, subjectToken)
	r.mu.Lock()
	r.sessions[id] = s
	r.mu.Unlock()
	return s
}

// Get returns the session for an id (false if missing or closed).
func (r *SessionRegistry) Get(id string) (*Session, bool) {
	r.mu.RLock()
	s, ok := r.sessions[id]
	r.mu.RUnlock()
	if !ok || s.closed.Load() {
		return nil, false
	}
	return s, true
}

// Close terminates a session by id. Idempotent.
func (r *SessionRegistry) Close(id string) {
	r.mu.Lock()
	s, ok := r.sessions[id]
	if ok {
		delete(r.sessions, id)
	}
	r.mu.Unlock()
	if ok {
		s.Close()
		r.fireClose(id)
	}
}

// CloseAll terminates every active session (used on server shutdown).
func (r *SessionRegistry) CloseAll() {
	r.mu.Lock()
	all := r.sessions
	r.sessions = make(map[string]*Session)
	r.mu.Unlock()
	for id, s := range all {
		s.Close()
		r.fireClose(id)
	}
}

func newSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: time-based id; the security of session ids matters in
		// production but a panic here would crash dev mode.
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> (i * 8))
		}
	}
	return "s_" + base64.RawURLEncoding.EncodeToString(b)
}
