package http

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
)

// serverRequestEnvelope is the JSON-RPC body shipped down the SSE
// channel as `event: server_request`. Mirrors a normal protocol.Request
// but is keyed on an id-space the server controls.
type serverRequestEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// pendingReq is one outstanding server-initiated request awaiting a
// client response. The ServerInitiatedRequester holds a map of these
// keyed on the unique request id.
type pendingReq struct {
	sessionID string
	resp      chan *protocol.Response
	expiresAt time.Time
}

// ServerInitiatedRequester ships server-initiated JSON-RPC requests over
// each session's SSE channel and waits for the matching response. The
// gateway calls Send to elicit; the POST /mcp handler calls TryDeliver
// when it sees an inbound JSON-RPC response with an id matching a
// pending entry.
//
// The struct is also a per-session mutex registry so concurrent
// elicitations against the same session cannot interleave (the spec is
// fine with concurrent IDs, but most clients render serially and a
// staggered queue is friendlier).
type ServerInitiatedRequester struct {
	sessions *mcpgw.SessionRegistry

	mu      sync.Mutex
	pending map[string]*pendingReq

	sessMu  sync.Mutex
	sessLks map[string]*sync.Mutex

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewServerInitiatedRequester constructs the requester and starts the
// background TTL sweeper.
func NewServerInitiatedRequester(sessions *mcpgw.SessionRegistry) *ServerInitiatedRequester {
	r := &ServerInitiatedRequester{
		sessions: sessions,
		pending:  make(map[string]*pendingReq),
		sessLks:  make(map[string]*sync.Mutex),
		stopCh:   make(chan struct{}),
	}
	r.wg.Add(1)
	go r.sweeper()
	return r
}

// Stop terminates the sweeper goroutine and rejects every pending
// request with a stream-disconnected error. Idempotent.
func (r *ServerInitiatedRequester) Stop() {
	r.stopOnce.Do(func() {
		close(r.stopCh)
		r.wg.Wait()
		r.mu.Lock()
		for id, p := range r.pending {
			select {
			case p.resp <- &protocol.Response{
				JSONRPC: protocol.JSONRPCVersion,
				ID:      json.RawMessage(`"` + id + `"`),
				Error:   protocol.NewError(protocol.ErrInternalError, "server shutting down", nil),
			}:
			default:
			}
			delete(r.pending, id)
		}
		r.mu.Unlock()
	})
}

// Send issues a server-initiated request to the session's SSE channel
// and blocks until the client responds (or timeout / disconnect).
func (r *ServerInitiatedRequester) Send(ctx context.Context, sessionID, method string, params any, timeout time.Duration) (*protocol.Response, error) {
	if r == nil || r.sessions == nil {
		return nil, errors.New("server-initiated: requester not configured")
	}
	sess, ok := r.sessions.Get(sessionID)
	if !ok {
		return nil, errors.New("server-initiated: session not found")
	}
	lock := r.sessionLock(sessionID)
	lock.Lock()
	defer lock.Unlock()

	id := newServerRequestID()
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("server-initiated: marshal params: %w", err)
	}
	envelope := serverRequestEnvelope{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"` + id + `"`),
		Method:  method,
		Params:  body,
	}
	rawEnvelope, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("server-initiated: marshal envelope: %w", err)
	}

	// Register the pending entry BEFORE pushing to the SSE channel so a
	// fast client cannot race the response in.
	respCh := make(chan *protocol.Response, 1)
	r.mu.Lock()
	r.pending[id] = &pendingReq{
		sessionID: sessionID,
		resp:      respCh,
		expiresAt: time.Now().Add(timeout),
	}
	r.mu.Unlock()
	defer r.removePending(id)

	// Reuse the session's notification channel for transport. We piggy-
	// back the envelope into a Notification's Method/Params with a
	// special method marker that the SSE writer recognises and emits as
	// `event: server_request` instead of `event: message`.
	if dropped := sess.EmitNotification(protocol.Notification{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  serverRequestNotifMethod,
		Params:  rawEnvelope,
	}); dropped {
		return nil, errors.New("server-initiated: session notification queue full or closed")
	}

	select {
	case resp := <-respCh:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, errors.New("server-initiated: timeout waiting for client response")
	}
}

// TryDeliver routes an inbound JSON-RPC response (no method, has id) to
// the pending entry whose id matches. Returns true when the message was
// consumed by a pending server-initiated request — the POST handler
// then short-circuits its normal request-dispatch path.
func (r *ServerInitiatedRequester) TryDeliver(body []byte) bool {
	if r == nil || len(body) == 0 {
		return false
	}
	var resp protocol.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return false
	}
	// JSON-RPC responses have no method and at least one of result/error.
	if resp.Result == nil && resp.Error == nil {
		return false
	}
	id, ok := unmarshalStringID(resp.ID)
	if !ok {
		return false
	}
	r.mu.Lock()
	pending, ok := r.pending[id]
	if ok {
		delete(r.pending, id)
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	pending.resp <- &resp
	return true
}

func (r *ServerInitiatedRequester) removePending(id string) {
	r.mu.Lock()
	delete(r.pending, id)
	r.mu.Unlock()
}

func (r *ServerInitiatedRequester) sessionLock(sessionID string) *sync.Mutex {
	r.sessMu.Lock()
	defer r.sessMu.Unlock()
	if lk, ok := r.sessLks[sessionID]; ok {
		return lk
	}
	lk := &sync.Mutex{}
	r.sessLks[sessionID] = lk
	return lk
}

func (r *ServerInitiatedRequester) sweeper() {
	defer r.wg.Done()
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case now := <-t.C:
			r.expireOlderThan(now)
		}
	}
}

func (r *ServerInitiatedRequester) expireOlderThan(now time.Time) {
	r.mu.Lock()
	expired := make([]string, 0)
	for id, p := range r.pending {
		if now.After(p.expiresAt) {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		p := r.pending[id]
		delete(r.pending, id)
		// Fire-and-forget timeout response. The Send goroutine has its
		// own timeout select, so this is just defensive cleanup.
		select {
		case p.resp <- &protocol.Response{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      json.RawMessage(`"` + id + `"`),
			Error:   protocol.NewError(protocol.ErrInternalError, "expired", nil),
		}:
		default:
		}
	}
	r.mu.Unlock()
}

// serverRequestNotifMethod is the marker the SSE writer keys on to emit
// `event: server_request`. Not a real MCP method — never reaches the
// wire under that name.
const serverRequestNotifMethod = "_portico/server_request"

// newServerRequestID returns a fresh identifier for a server-initiated
// request. Prefix `s_` keeps it disjoint from client-initiated ids.
func newServerRequestID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// Time-based fallback; the security of these ids is not security-
		// critical (only correlation matters).
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> (i * 8))
		}
	}
	return "s_" + base64.RawURLEncoding.EncodeToString(b)
}

// unmarshalStringID decodes a JSON-RPC id field into its string form.
// Numeric ids return ok=false (server-initiated ids are always strings).
func unmarshalStringID(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}
