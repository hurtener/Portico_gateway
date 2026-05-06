// Package stdio implements the southbound MCP Client over a child process's
// stdin/stdout. Stderr is forwarded to the supplied logger as Debug-level
// lines (most servers use it for human-readable startup output).
package stdio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/mcp/southbound"
)

// Config configures a stdio Client. ServerID is used in log lines and error
// data for routing diagnostics.
type Config struct {
	ServerID     string
	Command      string
	Args         []string
	Env          []string
	Cwd          string
	StartTimeout time.Duration
	Logger       *slog.Logger
}

// Client implements southbound.Client via a child process speaking JSON-RPC
// over stdio. Lines on stdout are JSON objects separated by '\n'.
type Client struct {
	cfg Config
	log *slog.Logger

	// process state
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	wg      sync.WaitGroup
	closeCh chan struct{}

	// JSON-RPC bookkeeping
	idCounter  atomic.Int64
	pendingMu  sync.Mutex
	pending    map[string]chan *protocol.Response
	progressMu sync.Mutex
	progressCB map[string]southbound.ProgressCallback // keyed on progress token (raw JSON string)

	// init state
	initOnce sync.Once
	initErr  error
	initDone atomic.Bool
	initRes  *protocol.InitializeResult

	writeMu sync.Mutex

	// notifications fan-out: every non-progress notification from the
	// downstream is published to notifCh with drop-oldest backpressure.
	notifCh chan protocol.Notification
}

// New constructs a Client. The process is NOT started until Start is called.
func New(cfg Config) *Client {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.StartTimeout == 0 {
		cfg.StartTimeout = 10 * time.Second
	}
	return &Client{
		cfg:        cfg,
		log:        cfg.Logger.With("server_id", cfg.ServerID, "transport", "stdio"),
		pending:    make(map[string]chan *protocol.Response),
		progressCB: make(map[string]southbound.ProgressCallback),
		closeCh:    make(chan struct{}),
		notifCh:    make(chan protocol.Notification, 32),
	}
}

// Start spawns the process and performs the MCP initialize handshake.
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
	cmd := exec.Command(c.cfg.Command, c.cfg.Args...) //nolint:gosec // operator-supplied command per server spec
	if c.cfg.Cwd != "" {
		cmd.Dir = c.cfg.Cwd
	}
	if len(c.cfg.Env) > 0 {
		cmd.Env = append(cmd.Environ(), c.cfg.Env...)
	}
	// Process group so we can kill children if the server forks helpers.
	cmd.SysProcAttr = setpgid()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn %q: %w", c.cfg.Command, err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout
	c.stderr = stderr

	c.wg.Add(2)
	go c.readLoop()
	go c.stderrLoop()

	startCtx, cancel := context.WithTimeout(ctx, c.cfg.StartTimeout)
	defer cancel()

	res, err := c.initialize(startCtx)
	if err != nil {
		_ = c.shutdown()
		return err
	}
	c.initRes = res

	// Send initialized notification per spec.
	if err := c.sendNotification(protocol.NotifInitialized, nil); err != nil {
		c.log.Warn("failed to send initialized notification", "err", err)
	}

	c.log.Info("stdio client ready",
		"server_name", res.ServerInfo.Name,
		"server_version", res.ServerInfo.Version,
		"protocol", res.ProtocolVersion)
	return nil
}

func (c *Client) initialize(ctx context.Context) (*protocol.InitializeResult, error) {
	params := protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.Implementation{
			Name:    "portico-gateway",
			Version: "phase-1",
		},
	}
	raw, err := c.call(ctx, protocol.MethodInitialize, params)
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}
	var res protocol.InitializeResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("initialize result: %w", err)
	}
	return &res, nil
}

// Initialized reports whether Start has succeeded.
func (c *Client) Initialized() bool { return c.initDone.Load() }

// Capabilities returns the post-handshake server capabilities.
func (c *Client) Capabilities() protocol.ServerCapabilities {
	if c.initRes == nil {
		return protocol.ServerCapabilities{}
	}
	return c.initRes.Capabilities
}

// ServerInfo returns the downstream's identification.
func (c *Client) ServerInfo() protocol.Implementation {
	if c.initRes == nil {
		return protocol.Implementation{}
	}
	return c.initRes.ServerInfo
}

// Ping issues an MCP ping. Result body is empty per spec.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.call(ctx, protocol.MethodPing, struct{}{})
	return err
}

// ListTools fetches the downstream tool catalog. Phase 1 does not paginate;
// it relies on the downstream returning the full set in one response.
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

// ListResources returns the downstream resource catalog. Pass cursor=""
// for the first page; cursor is opaque to Portico.
func (c *Client) ListResources(ctx context.Context, cursor string) ([]protocol.Resource, string, error) {
	raw, err := c.call(ctx, protocol.MethodResourcesList, protocol.ListResourcesParams{Cursor: cursor})
	if err != nil {
		return nil, "", err
	}
	var res protocol.ListResourcesResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, "", fmt.Errorf("resources/list result: %w", err)
	}
	return res.Resources, res.NextCursor, nil
}

// ListResourceTemplates returns the parameterised-URI catalog.
func (c *Client) ListResourceTemplates(ctx context.Context, cursor string) ([]protocol.ResourceTemplate, string, error) {
	raw, err := c.call(ctx, protocol.MethodResourcesTemplatesList, protocol.ListResourceTemplatesParams{Cursor: cursor})
	if err != nil {
		return nil, "", err
	}
	var res protocol.ListResourceTemplatesResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, "", fmt.Errorf("resources/templates/list result: %w", err)
	}
	return res.ResourceTemplates, res.NextCursor, nil
}

// ReadResource fetches the bytes for a downstream resource URI.
func (c *Client) ReadResource(ctx context.Context, uri string) (*protocol.ReadResourceResult, error) {
	raw, err := c.call(ctx, protocol.MethodResourcesRead, protocol.ReadResourceParams{URI: uri})
	if err != nil {
		return nil, err
	}
	var res protocol.ReadResourceResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("resources/read result: %w", err)
	}
	return &res, nil
}

// SubscribeResource asks the downstream to publish updates for uri.
func (c *Client) SubscribeResource(ctx context.Context, uri string) error {
	_, err := c.call(ctx, protocol.MethodResourcesSubscribe, protocol.SubscribeResourceParams{URI: uri})
	return err
}

// UnsubscribeResource cancels a prior SubscribeResource.
func (c *Client) UnsubscribeResource(ctx context.Context, uri string) error {
	_, err := c.call(ctx, protocol.MethodResourcesUnsubscribe, protocol.UnsubscribeResourceParams{URI: uri})
	return err
}

// ListPrompts returns the downstream prompt catalog.
func (c *Client) ListPrompts(ctx context.Context, cursor string) ([]protocol.Prompt, string, error) {
	raw, err := c.call(ctx, protocol.MethodPromptsList, protocol.ListPromptsParams{Cursor: cursor})
	if err != nil {
		return nil, "", err
	}
	var res protocol.ListPromptsResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, "", fmt.Errorf("prompts/list result: %w", err)
	}
	return res.Prompts, res.NextCursor, nil
}

// GetPrompt renders a prompt with the supplied arguments.
func (c *Client) GetPrompt(ctx context.Context, name string, args map[string]string) (*protocol.GetPromptResult, error) {
	raw, err := c.call(ctx, protocol.MethodPromptsGet, protocol.GetPromptParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	var res protocol.GetPromptResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("prompts/get result: %w", err)
	}
	return &res, nil
}

// Notifications exposes the downstream's notifications stream. Drop-oldest
// on backpressure (32-deep buffer); consumers must drain promptly.
func (c *Client) Notifications() <-chan protocol.Notification { return c.notifCh }

// CallTool routes to the downstream and surfaces progress notifications via
// the provided callback.
func (c *Client) CallTool(ctx context.Context, name string, arguments json.RawMessage, progressToken json.RawMessage, progress southbound.ProgressCallback) (*protocol.CallToolResult, error) {
	params := protocol.CallToolParams{Name: name, Arguments: arguments}
	if len(progressToken) > 0 {
		// Pass the client-supplied token through unchanged.
		meta := map[string]json.RawMessage{"progressToken": progressToken}
		mb, _ := json.Marshal(meta)
		params.Meta = mb

		// Register the progress callback keyed on the token's JSON form.
		c.progressMu.Lock()
		c.progressCB[string(progressToken)] = progress
		c.progressMu.Unlock()
		defer func() {
			c.progressMu.Lock()
			delete(c.progressCB, string(progressToken))
			c.progressMu.Unlock()
		}()
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

// call sends a JSON-RPC request and waits for the matching response.
// On ctx cancel the call returns ctx.Err() and emits notifications/cancelled
// to the downstream so it can stop work.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID()
	idJSON := []byte(fmt.Sprintf("%d", id))
	resp := make(chan *protocol.Response, 1)
	c.pendingMu.Lock()
	c.pending[string(idJSON)] = resp
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, string(idJSON))
		c.pendingMu.Unlock()
	}()

	pBody, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	req := protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      idJSON,
		Method:  method,
		Params:  pBody,
	}
	if err := c.writeMessage(req); err != nil {
		return nil, err
	}

	select {
	case r := <-resp:
		if r.Error != nil {
			return nil, r.Error
		}
		return r.Result, nil
	case <-ctx.Done():
		// Forward cancellation to the downstream.
		_ = c.sendNotification(protocol.NotifCancelled, protocol.CancelledParams{
			RequestID: idJSON,
			Reason:    ctx.Err().Error(),
		})
		return nil, ctx.Err()
	case <-c.closeCh:
		return nil, errors.New("stdio: client closed")
	}
}

func (c *Client) sendNotification(method string, params any) error {
	var body json.RawMessage
	if params != nil {
		var err error
		body, err = json.Marshal(params)
		if err != nil {
			return err
		}
	}
	notif := protocol.Notification{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  method,
		Params:  body,
	}
	return c.writeMessage(notif)
}

func (c *Client) writeMessage(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = c.stdin.Write(b)
	return err
}

func (c *Client) readLoop() {
	defer c.wg.Done()
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		c.handleIncoming(line)
	}
	if err := scanner.Err(); err != nil {
		c.log.Warn("stdout scanner error", "err", err)
	}
	// Stream ended: fail any in-flight requests.
	c.failAllPending(errors.New("stdio: process closed stdout"))
}

func (c *Client) handleIncoming(line []byte) {
	// Probe for "id" presence: if present and not null, it's a Response;
	// otherwise it's a Notification (no id) or a Request from the server
	// (which Phase 1 doesn't expect from stdio downstream — log & ignore).
	var probe struct {
		ID     json.RawMessage `json:"id,omitempty"`
		Method string          `json:"method,omitempty"`
		Result json.RawMessage `json:"result,omitempty"`
		Error  *protocol.Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		c.log.Warn("malformed downstream message", "err", err)
		return
	}
	switch {
	case probe.Method != "" && len(probe.ID) == 0:
		c.handleNotification(line, probe.Method)
	case len(probe.ID) > 0 && string(probe.ID) != "null":
		c.handleResponse(line)
	default:
		c.log.Debug("downstream sent unhandled message", "method", probe.Method)
	}
}

func (c *Client) handleResponse(line []byte) {
	var resp protocol.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		c.log.Warn("malformed response", "err", err)
		return
	}
	c.pendingMu.Lock()
	ch, ok := c.pending[string(resp.ID)]
	c.pendingMu.Unlock()
	if !ok {
		c.log.Debug("response for unknown id", "id", string(resp.ID))
		return
	}
	select {
	case ch <- &resp:
	default:
		c.log.Warn("response channel full", "id", string(resp.ID))
	}
}

func (c *Client) handleNotification(line []byte, method string) {
	var n protocol.Notification
	if err := json.Unmarshal(line, &n); err != nil {
		c.log.Warn("malformed notification", "err", err)
		return
	}
	if method == protocol.NotifProgress {
		var p protocol.ProgressParams
		if err := json.Unmarshal(n.Params, &p); err != nil {
			return
		}
		c.progressMu.Lock()
		cb, ok := c.progressCB[string(p.ProgressToken)]
		c.progressMu.Unlock()
		if !ok || cb == nil {
			return
		}
		cb(p)
		return
	}
	// Everything else (list_changed, resources/updated, etc.) goes to the
	// notifications channel for the list-changed mux to consume. Drop-oldest
	// on backpressure: a slow consumer is the consumer's problem, not the
	// downstream's.
	c.publishNotification(n)
}

func (c *Client) publishNotification(n protocol.Notification) {
	// Single producer (readLoop): try to send, and if the buffer is full,
	// drop one stale entry to make room. One drain attempt is enough
	// because nobody else can refill between drain and the retry send.
	select {
	case c.notifCh <- n:
		return
	default:
	}
	select {
	case <-c.notifCh:
	default:
	}
	select {
	case c.notifCh <- n:
	default:
	}
}

func (c *Client) stderrLoop() {
	defer c.wg.Done()
	scanner := bufio.NewScanner(c.stderr)
	for scanner.Scan() {
		c.log.Debug("downstream stderr", "line", scanner.Text())
	}
}

func (c *Client) failAllPending(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pending {
		select {
		case ch <- &protocol.Response{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      json.RawMessage(id),
			Error:   protocol.NewError(protocol.ErrUpstreamUnavailable, err.Error(), nil),
		}:
		default:
		}
	}
	c.pending = make(map[string]chan *protocol.Response)
}

func (c *Client) nextID() int64 {
	return c.idCounter.Add(1)
}

// Close terminates the child process and joins reader goroutines.
func (c *Client) Close(ctx context.Context) error {
	return c.shutdown()
}

func (c *Client) shutdown() error {
	select {
	case <-c.closeCh:
		// already closing
	default:
		close(c.closeCh)
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		// Try graceful first; SIGKILL if needed.
		_ = killGroup(c.cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			_ = c.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = killGroup(c.cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
	}
	c.wg.Wait()
	return nil
}
