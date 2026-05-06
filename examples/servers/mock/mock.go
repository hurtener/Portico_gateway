// Package mock provides an in-process and standalone-binary MCP server for
// testing Portico's southbound. The implementation is deliberately minimal:
// initialize handshake, tools/list, tools/call with a small repertoire
// (echo, add, slow-with-progress, error). Phase 3+ tests will extend this
// with resources/prompts.
package mock

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// Server is a single instance of the mock MCP server.
type Server struct {
	Name    string
	Version string
	// ToolsOverride lets a test customise the advertised tools. Default is
	// the four-tool repertoire (echo, add, slow, broken).
	ToolsOverride []protocol.Tool

	// Phase 3: optional resource + prompt fixtures. nil means "no
	// resources / prompts advertised" and the corresponding methods
	// return ErrMethodNotFound so aggregators silently skip the server.
	ResourcesOverride         []protocol.Resource
	ResourceTemplatesOverride []protocol.ResourceTemplate
	PromptsOverride           []protocol.Prompt
	// ReadOverride lets a test inject custom resources/read responses.
	ReadOverride func(uri string) (*protocol.ReadResourceResult, error)
	// GetPromptOverride lets a test customise prompts/get.
	GetPromptOverride func(name string, args map[string]string) (*protocol.GetPromptResult, error)
}

// NewDefault returns a Server ready to run against a stdio pipe.
func NewDefault(name string) *Server {
	if name == "" {
		name = "mockmcp"
	}
	return &Server{Name: name, Version: "0.1.0"}
}

// Run reads JSON-RPC requests from in and writes responses to out. Blocks
// until in is closed or ctx is cancelled.
func (s *Server) Run(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	enc := newLineEncoder(out)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		s.handleLine(ctx, line, enc)
	}
	return scanner.Err()
}

func (s *Server) handleLine(ctx context.Context, line []byte, enc *lineEncoder) {
	var probe struct {
		ID     json.RawMessage `json:"id,omitempty"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params,omitempty"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		// Malformed; ignore (real servers may send back an error).
		return
	}
	isNotification := len(probe.ID) == 0 || string(probe.ID) == "null"

	if isNotification {
		// Phase 1: nothing to do for client notifications.
		return
	}

	resp := protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: probe.ID}
	switch probe.Method {
	case protocol.MethodInitialize:
		caps := protocol.ServerCapabilities{Tools: &protocol.ToolsCapability{ListChanged: true}}
		if len(s.ResourcesOverride) > 0 || s.ReadOverride != nil {
			caps.Resources = &protocol.ResourcesCapability{ListChanged: true}
		}
		if len(s.PromptsOverride) > 0 || s.GetPromptOverride != nil {
			caps.Prompts = &protocol.PromptsCapability{ListChanged: true}
		}
		body, _ := json.Marshal(protocol.InitializeResult{
			ProtocolVersion: protocol.ProtocolVersion,
			Capabilities:    caps,
			ServerInfo:      protocol.Implementation{Name: s.Name, Version: s.Version},
		})
		resp.Result = body
	case protocol.MethodPing:
		resp.Result = json.RawMessage(`{}`)
	case protocol.MethodToolsList:
		body, _ := json.Marshal(protocol.ListToolsResult{Tools: s.tools()})
		resp.Result = body
	case protocol.MethodToolsCall:
		s.handleCallTool(ctx, probe.ID, probe.Params, enc)
		return
	case protocol.MethodResourcesList:
		if s.ResourcesOverride == nil {
			resp.Error = protocol.NewError(protocol.ErrMethodNotFound, "resources not supported", nil)
			break
		}
		body, _ := json.Marshal(protocol.ListResourcesResult{Resources: s.ResourcesOverride})
		resp.Result = body
	case protocol.MethodResourcesTemplatesList:
		if s.ResourceTemplatesOverride == nil {
			resp.Error = protocol.NewError(protocol.ErrMethodNotFound, "templates not supported", nil)
			break
		}
		body, _ := json.Marshal(protocol.ListResourceTemplatesResult{ResourceTemplates: s.ResourceTemplatesOverride})
		resp.Result = body
	case protocol.MethodResourcesRead:
		var p protocol.ReadResourceParams
		_ = json.Unmarshal(probe.Params, &p)
		res, err := s.handleRead(p.URI)
		if err != nil {
			resp.Error = protocol.NewError(protocol.ErrInternalError, err.Error(), nil)
			break
		}
		body, _ := json.Marshal(res)
		resp.Result = body
	case protocol.MethodPromptsList:
		if s.PromptsOverride == nil {
			resp.Error = protocol.NewError(protocol.ErrMethodNotFound, "prompts not supported", nil)
			break
		}
		body, _ := json.Marshal(protocol.ListPromptsResult{Prompts: s.PromptsOverride})
		resp.Result = body
	case protocol.MethodPromptsGet:
		var p protocol.GetPromptParams
		_ = json.Unmarshal(probe.Params, &p)
		res, err := s.handleGetPrompt(p.Name, p.Arguments)
		if err != nil {
			resp.Error = protocol.NewError(protocol.ErrInternalError, err.Error(), nil)
			break
		}
		body, _ := json.Marshal(res)
		resp.Result = body
	default:
		resp.Error = protocol.NewError(protocol.ErrMethodNotFound, "unknown method", map[string]string{"method": probe.Method})
	}
	enc.encode(resp)
}

func (s *Server) handleRead(uri string) (*protocol.ReadResourceResult, error) {
	if s.ReadOverride != nil {
		return s.ReadOverride(uri)
	}
	for _, r := range s.ResourcesOverride {
		if r.URI == uri {
			return &protocol.ReadResourceResult{Contents: []protocol.ResourceContent{
				{URI: uri, MimeType: r.MimeType, Text: "mock body for " + uri},
			}}, nil
		}
	}
	return nil, fmt.Errorf("unknown resource %q", uri)
}

func (s *Server) handleGetPrompt(name string, args map[string]string) (*protocol.GetPromptResult, error) {
	if s.GetPromptOverride != nil {
		return s.GetPromptOverride(name, args)
	}
	return &protocol.GetPromptResult{
		Description: "mock prompt " + name,
		Messages: []protocol.PromptMessage{
			{Role: "user", Content: protocol.ContentBlock{Type: "text", Text: "render " + name}},
		},
	}, nil
}

func (s *Server) handleCallTool(ctx context.Context, id json.RawMessage, paramsRaw json.RawMessage, enc *lineEncoder) {
	var p protocol.CallToolParams
	if err := json.Unmarshal(paramsRaw, &p); err != nil {
		enc.encode(protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: id,
			Error: protocol.NewError(protocol.ErrInvalidParams, err.Error(), nil)})
		return
	}
	switch p.Name {
	case "echo":
		var in struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(p.Arguments, &in)
		body, _ := json.Marshal(protocol.CallToolResult{
			Content: []protocol.ContentBlock{{Type: "text", Text: in.Message}},
		})
		enc.encode(protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: id, Result: body})
	case "add":
		var args struct {
			A int `json:"a"`
			B int `json:"b"`
		}
		_ = json.Unmarshal(p.Arguments, &args)
		body, _ := json.Marshal(protocol.CallToolResult{
			Content: []protocol.ContentBlock{{Type: "text", Text: fmt.Sprintf("%d", args.A+args.B)}},
		})
		enc.encode(protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: id, Result: body})
	case "slow":
		s.handleSlow(ctx, id, p, enc)
	case "broken":
		enc.encode(protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: id,
			Error: protocol.NewError(protocol.ErrInternalError, "intentional failure", nil)})
	default:
		enc.encode(protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: id,
			Error: protocol.NewError(-32601, "unknown tool", map[string]string{"name": p.Name})})
	}
}

func (s *Server) handleSlow(ctx context.Context, id json.RawMessage, p protocol.CallToolParams, enc *lineEncoder) {
	var args struct {
		DurationMs int `json:"duration_ms"`
	}
	_ = json.Unmarshal(p.Arguments, &args)
	if args.DurationMs <= 0 {
		args.DurationMs = 60
	}

	progressToken := extractProgressToken(p.Meta)
	step := time.Duration(args.DurationMs) * time.Millisecond / 4
	for i := 1; i <= 4; i++ {
		select {
		case <-ctx.Done():
			enc.encode(protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: id,
				Error: protocol.NewError(protocol.ErrCancelled, "cancelled", nil)})
			return
		case <-time.After(step):
		}
		if len(progressToken) > 0 {
			pp := protocol.ProgressParams{
				ProgressToken: progressToken,
				Progress:      float64(i),
			}
			total := float64(4)
			pp.Total = &total
			body, _ := json.Marshal(pp)
			enc.encode(protocol.Notification{JSONRPC: protocol.JSONRPCVersion, Method: protocol.NotifProgress, Params: body})
		}
	}
	body, _ := json.Marshal(protocol.CallToolResult{
		Content: []protocol.ContentBlock{{Type: "text", Text: "done"}},
	})
	enc.encode(protocol.Response{JSONRPC: protocol.JSONRPCVersion, ID: id, Result: body})
}

func extractProgressToken(meta json.RawMessage) json.RawMessage {
	if len(meta) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(meta, &m); err != nil {
		return nil
	}
	return m["progressToken"]
}

func (s *Server) tools() []protocol.Tool {
	if s.ToolsOverride != nil {
		return s.ToolsOverride
	}
	return defaultTools()
}

func defaultTools() []protocol.Tool {
	return []protocol.Tool{
		{
			Name:        "echo",
			Description: "Echo a message back",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`),
		},
		{
			Name:        "add",
			Description: "Add two integers",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"integer"},"b":{"type":"integer"}},"required":["a","b"]}`),
		},
		{
			Name:        "slow",
			Description: "Simulates a slow tool that emits progress notifications",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"duration_ms":{"type":"integer","minimum":1}}}`),
		},
		{
			Name:        "broken",
			Description: "Always returns an error",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		},
	}
}

// ----- writer with newline framing ---------------------------------------

type lineEncoder struct {
	out io.Writer
}

func newLineEncoder(w io.Writer) *lineEncoder { return &lineEncoder{out: w} }

func (e *lineEncoder) encode(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	b = append(b, '\n')
	_, _ = e.out.Write(b)
	if f, ok := e.out.(interface{ Sync() error }); ok {
		_ = f.Sync()
	}
}

// stringer for nicer test diffs.
var _ = strings.Builder{}
