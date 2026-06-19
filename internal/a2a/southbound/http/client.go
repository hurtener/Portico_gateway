// Package http implements the southbound A2A Client over HTTP. A2A is a
// JSON-RPC 2.0 protocol carried over plain HTTP POST. Phase 16 ships the
// unary surface only — agent-card fetch, message/send, tasks/get,
// tasks/cancel. Streaming methods (message/stream, tasks/resubscribe,
// SSE) and the per-(tenant, peer) manager pool land in separate later
// units; do NOT build them here (§4.2 unit P16-C scope).
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	nethttp "net/http"
	"strconv"
	"sync/atomic"
	"time"

	a2a "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
	"github.com/hurtener/Portico_gateway/internal/a2a/southbound"
	"github.com/hurtener/Portico_gateway/internal/telemetry"
)

// Config configures an HTTP Client for a single downstream A2A peer.
type Config struct {
	// PeerID is the registry ID of the peer (used for logging + transport
	// error messages). It is NEVER sent on the wire.
	PeerID string
	// Endpoint is the peer's JSON-RPC POST URL — typically the `url`
	// field of its agent card.
	Endpoint string
	// AuthHeader is a static Authorization header value (e.g.
	// "Bearer …"). HeaderProvider, when set, takes precedence on
	// outbound requests. AuthHeader is NEVER logged.
	AuthHeader string
	// Timeout is the per-call HTTP timeout. Zero means 30s.
	Timeout time.Duration
	// Logger receives peer-level request/log records. Nil → slog.Default().
	Logger *slog.Logger
	// HTTPClient is an optional *nethttp.Client injection; useful for
	// tests. Nil → a fresh *nethttp.Client with cfg.Timeout is built.
	HTTPClient *nethttp.Client
	// HeaderProvider, when set, is called on every outbound request and
	// its returned headers merged onto the request (overriding AuthHeader
	// on `Authorization`). The provider receives the per-request context
	// so it can honour cancellation while resolving credentials. Errors
	// abort the request with a transport error.
	HeaderProvider func(ctx context.Context) (map[string]string, error)
}

// Client is the HTTP-backed A2A peer client. One Client per peer; it is
// goroutine-safe: every call independently updates the id counter and
// marshals its own body. There is no session state — A2A HTTP is a
// stateless per-call transport.
type Client struct {
	cfg Config
	log *slog.Logger
	hc  *nethttp.Client

	idCounter atomic.Int64
}

// Compile-time guarantee that *Client satisfies southbound.Client.
var _ southbound.Client = (*Client)(nil)

// New builds a Client with sensible defaults applied.
func New(cfg Config) *Client {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &nethttp.Client{Timeout: cfg.Timeout}
	}
	return &Client{
		cfg: cfg,
		log: cfg.Logger.With("peer_id", cfg.PeerID, "transport", "http"),
		hc:  hc,
	}
}

// FetchAgentCard GETs the peer's well-known agent-card URL. A2A's
// discovery endpoint is a plain HTTP GET, NOT a JSON-RPC call. Decoded
// straight into *a2a.AgentCard.
func (c *Client) FetchAgentCard(ctx context.Context, cardURL string) (*a2a.AgentCard, error) {
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, cardURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build agent-card request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.cfg.AuthHeader != "" {
		req.Header.Set("Authorization", c.cfg.AuthHeader)
	}
	if c.cfg.HeaderProvider != nil {
		hdrs, perr := c.cfg.HeaderProvider(ctx)
		if perr != nil {
			return nil, &transportError{peer: c.cfg.PeerID, err: fmt.Errorf("header provider: %w", perr)}
		}
		for k, v := range hdrs {
			req.Header.Set(k, v)
		}
	}
	telemetry.InjectIntoHTTP(ctx, req.Header)

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, &transportError{peer: c.cfg.PeerID, err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		return nil, &transportError{peer: c.cfg.PeerID, err: fmt.Errorf("http %d: %s", resp.StatusCode, string(raw))}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8 MiB cap
	if err != nil {
		return nil, &transportError{peer: c.cfg.PeerID, err: err}
	}
	var card a2a.AgentCard
	if err := json.Unmarshal(body, &card); err != nil {
		return nil, fmt.Errorf("agent card decode: %w", err)
	}
	return &card, nil
}

// SendMessage issues a unary message/send against the peer. The A2A
// spec allows the result to be either a Task or a Message, so the raw
// JSON result is returned for the caller to decode against the expected
// shape.
func (c *Client) SendMessage(ctx context.Context, params a2a.MessageSendParams) (json.RawMessage, error) {
	return c.call(ctx, a2a.MethodMessageSend, params)
}

// GetTask issues tasks/get and decodes the Task.
func (c *Client) GetTask(ctx context.Context, params a2a.TaskQueryParams) (*a2a.Task, error) {
	raw, err := c.call(ctx, a2a.MethodTasksGet, params)
	if err != nil {
		return nil, err
	}
	var t a2a.Task
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("tasks/get decode: %w", err)
	}
	return &t, nil
}

// CancelTask issues tasks/cancel and decodes the updated Task.
func (c *Client) CancelTask(ctx context.Context, params a2a.TaskIDParams) (*a2a.Task, error) {
	raw, err := c.call(ctx, a2a.MethodTasksCancel, params)
	if err != nil {
		return nil, err
	}
	var t a2a.Task
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("tasks/cancel decode: %w", err)
	}
	return &t, nil
}

// Close releases the client's resources. A2A HTTP transport is stateless
// (no TCP/stream ownership beyond per-call reuse by net/http itself), so
// there is nothing to release; we return nil. Sub-sequent calls would
// continue to work; callers are expected to drop the reference.
func (c *Client) Close(_ context.Context) error {
	return nil
}

// call builds an a2a.Request with a fresh id, POSTs it to cfg.Endpoint
// as a JSON-RPC 2.0 call, and returns the raw `result` from the
// response. A non-nil `error` field on the response is returned as the
// underlying *a2a.Error so callers can errors.As it. Transport-level
// failures (non-2xx, dial errors, body decode failure) are wrapped in
// *transportError so AsProtocolError can distinguish them.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	var pBody json.RawMessage
	if params != nil {
		var mErr error
		pBody, mErr = json.Marshal(params)
		if mErr != nil {
			return nil, fmt.Errorf("marshal params: %w", mErr)
		}
	}
	idJSON := []byte(strconv.FormatInt(c.idCounter.Add(1), 10))
	req := a2a.Request{
		JSONRPC: a2a.JSONRPCVersion,
		ID:      idJSON,
		Method:  method,
		Params:  pBody,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodPost, c.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if c.cfg.AuthHeader != "" {
		httpReq.Header.Set("Authorization", c.cfg.AuthHeader)
	}
	if c.cfg.HeaderProvider != nil {
		hdrs, perr := c.cfg.HeaderProvider(ctx)
		if perr != nil {
			return nil, &transportError{peer: c.cfg.PeerID, err: fmt.Errorf("header provider: %w", perr)}
		}
		for k, v := range hdrs {
			httpReq.Header.Set(k, v)
		}
	}
	telemetry.InjectIntoHTTP(ctx, httpReq.Header)

	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return nil, &transportError{peer: c.cfg.PeerID, err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		return nil, &transportError{peer: c.cfg.PeerID, err: fmt.Errorf("http %d: %s", resp.StatusCode, string(raw))}
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8 MiB cap
	if err != nil {
		return nil, &transportError{peer: c.cfg.PeerID, err: err}
	}
	var r a2a.Response
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, fmt.Errorf("malformed json-rpc response: %w", err)
	}
	if r.Error != nil {
		return nil, r.Error
	}
	c.log.Debug("a2a call ok", "method", method)
	return r.Result, nil
}

// transportError marks a non-protocol (HTTP transport layer) failure.
// It implements `errors.Is`/`errors.As` via Unwrap so callers can
// recover the underlying cause.
type transportError struct {
	peer string
	err  error
}

func (e *transportError) Error() string { return fmt.Sprintf("a2a peer %s: %v", e.peer, e.err) }
func (e *transportError) Unwrap() error { return e.err }

// AsProtocolError converts a southbound error into an *a2a.Error suitable
// for echoing to the northbound caller. Existing *a2a.Error values (from
// JSON-RPC error responses) are returned unchanged; transport-level
// failures collapse to a2a.ErrInternalError (A2A has no
// upstream-unavailable code).
func AsProtocolError(err error) *a2a.Error {
	if err == nil {
		return nil
	}
	var pe *a2a.Error
	if errors.As(err, &pe) {
		return pe
	}
	return &a2a.Error{Code: a2a.ErrInternalError, Message: err.Error()}
}
