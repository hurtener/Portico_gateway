// Package http implements the southbound MCP Client over HTTP. Phase 1
// supports the JSON-response variant of Streamable HTTP (POST /mcp returns
// application/json). SSE variant + long-lived progress stream land later
// when a real downstream needs them; for the V0.1 demo path the JSON-only
// variant is sufficient.
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
	"sync"
	"sync/atomic"
	"time"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/mcp/southbound"
)

// Config configures an HTTP Client.
type Config struct {
	ServerID   string
	URL        string
	AuthHeader string // Phase 5 wires real values from the vault
	Timeout    time.Duration
	Logger     *slog.Logger
	HTTPClient *nethttp.Client // optional injection for tests
}

type Client struct {
	cfg Config
	log *slog.Logger
	hc  *nethttp.Client

	idCounter atomic.Int64

	mu        sync.Mutex
	sessionID string

	initOnce sync.Once
	initErr  error
	initRes  *protocol.InitializeResult
	initDone atomic.Bool
}

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
		log: cfg.Logger.With("server_id", cfg.ServerID, "transport", "http"),
		hc:  hc,
	}
}

// Start performs the MCP initialize handshake.
func (c *Client) Start(ctx context.Context) error {
	c.initOnce.Do(func() {
		c.initErr = c.bootstrap(ctx)
		if c.initErr == nil {
			c.initDone.Store(true)
		}
	})
	return c.initErr
}

func (c *Client) bootstrap(ctx context.Context) error {
	params := protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo:      protocol.Implementation{Name: "portico-gateway", Version: "phase-1"},
	}
	raw, err := c.call(ctx, protocol.MethodInitialize, params)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	var res protocol.InitializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("initialize result: %w", err)
	}
	c.initRes = &res
	// Send initialized notification.
	if err := c.notify(ctx, protocol.NotifInitialized, nil); err != nil {
		c.log.Warn("initialized notification failed (continuing)", "err", err)
	}
	c.log.Info("http client ready", "server_name", res.ServerInfo.Name, "protocol", res.ProtocolVersion)
	return nil
}

func (c *Client) Initialized() bool { return c.initDone.Load() }

func (c *Client) Capabilities() protocol.ServerCapabilities {
	if c.initRes == nil {
		return protocol.ServerCapabilities{}
	}
	return c.initRes.Capabilities
}

func (c *Client) ServerInfo() protocol.Implementation {
	if c.initRes == nil {
		return protocol.Implementation{}
	}
	return c.initRes.ServerInfo
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.call(ctx, protocol.MethodPing, struct{}{})
	return err
}

func (c *Client) ListTools(ctx context.Context) ([]protocol.Tool, error) {
	raw, err := c.call(ctx, protocol.MethodToolsList, protocol.ListToolsParams{})
	if err != nil {
		return nil, err
	}
	var res protocol.ListToolsResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("tools/list result: %w", err)
	}
	return res.Tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, arguments json.RawMessage, progressToken json.RawMessage, _ southbound.ProgressCallback) (*protocol.CallToolResult, error) {
	// Phase 1 does not subscribe to SSE progress streams from HTTP downstreams;
	// progress notifications drop on the floor for HTTP transport. The
	// progressToken is forwarded to the downstream so a future SSE-aware
	// implementation can correlate.
	params := protocol.CallToolParams{Name: name, Arguments: arguments}
	if len(progressToken) > 0 {
		meta := map[string]json.RawMessage{"progressToken": progressToken}
		mb, _ := json.Marshal(meta)
		params.Meta = mb
	}
	raw, err := c.call(ctx, protocol.MethodToolsCall, params)
	if err != nil {
		return nil, err
	}
	var res protocol.CallToolResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("tools/call result: %w", err)
	}
	return &res, nil
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.idCounter.Add(1)
	idJSON := []byte(fmt.Sprintf("%d", id))
	pBody, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	req := protocol.Request{JSONRPC: protocol.JSONRPCVersion, ID: idJSON, Method: method, Params: pBody}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodPost, c.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	if c.cfg.AuthHeader != "" {
		httpReq.Header.Set("Authorization", c.cfg.AuthHeader)
	}
	c.mu.Lock()
	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	c.mu.Unlock()

	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return nil, &transportError{server: c.cfg.ServerID, err: err}
	}
	defer resp.Body.Close()

	// Capture session id from initialize response.
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.mu.Lock()
		c.sessionID = sid
		c.mu.Unlock()
	}

	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
		return nil, &transportError{server: c.cfg.ServerID, err: fmt.Errorf("http %d: %s", resp.StatusCode, string(raw))}
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "text/event-stream" {
		return nil, errors.New("http: SSE response variant not supported in Phase 1")
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8 MiB cap
	if err != nil {
		return nil, &transportError{server: c.cfg.ServerID, err: err}
	}
	var r protocol.Response
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, fmt.Errorf("malformed response: %w", err)
	}
	if r.Error != nil {
		return nil, r.Error
	}
	return r.Result, nil
}

func (c *Client) notify(ctx context.Context, method string, params any) error {
	var pBody json.RawMessage
	if params != nil {
		var err error
		pBody, err = json.Marshal(params)
		if err != nil {
			return err
		}
	}
	notif := protocol.Notification{JSONRPC: protocol.JSONRPCVersion, Method: method, Params: pBody}
	body, _ := json.Marshal(notif)
	httpReq, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodPost, c.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.cfg.AuthHeader != "" {
		httpReq.Header.Set("Authorization", c.cfg.AuthHeader)
	}
	c.mu.Lock()
	if c.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	c.mu.Unlock()
	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return nil
}

// Close on HTTP transport sends DELETE /mcp to terminate the upstream session.
func (c *Client) Close(ctx context.Context) error {
	c.mu.Lock()
	sid := c.sessionID
	c.mu.Unlock()
	if sid == "" {
		return nil
	}
	httpReq, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodDelete, c.cfg.URL, nil)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Mcp-Session-Id", sid)
	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return nil
}

// transportError signals that the failure was at the transport (not protocol) layer.
type transportError struct {
	server string
	err    error
}

func (e *transportError) Error() string { return fmt.Sprintf("upstream %s: %v", e.server, e.err) }
func (e *transportError) Unwrap() error { return e.err }

// AsProtocolError converts a southbound error into a JSON-RPC *protocol.Error
// suitable for echoing to the northbound client.
func AsProtocolError(err error) *protocol.Error {
	if err == nil {
		return nil
	}
	var pe *protocol.Error
	if errors.As(err, &pe) {
		return pe
	}
	return protocol.NewError(protocol.ErrUpstreamUnavailable, err.Error(), nil)
}
