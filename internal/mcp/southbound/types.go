// Package southbound is the protocol Portico speaks *outward* to downstream
// MCP servers. Concrete transports live in subdirectories (stdio, http);
// callers depend on the Client interface, not the concrete type.
package southbound

import (
	"context"
	"encoding/json"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// ProgressCallback is invoked for every progress notification a downstream
// emits during a CallTool. Phase 1 forwards these to the northbound session's
// notification channel.
type ProgressCallback func(protocol.ProgressParams)

// Client is the northbound-facing surface of any downstream MCP server.
// Phase 1 implements tools (init/list/call); Phase 3 fills in resources +
// prompts.
type Client interface {
	// Start performs the MCP initialize handshake. Must be called before any
	// other method; safe to call multiple times (idempotent — subsequent calls
	// return the cached InitializeResult).
	Start(ctx context.Context) error

	// Initialized reports whether Start has completed successfully.
	Initialized() bool

	// Capabilities returns the server's advertised capabilities (post-init).
	Capabilities() protocol.ServerCapabilities

	// ServerInfo returns the downstream server's name + version.
	ServerInfo() protocol.Implementation

	// Ping is a cheap liveness check.
	Ping(ctx context.Context) error

	// ListTools returns the downstream tool catalog. Caches are caller's job.
	ListTools(ctx context.Context) ([]protocol.Tool, error)

	// CallTool invokes a tool. progress (may be nil) is called for every
	// downstream notifications/progress event tied to this call.
	CallTool(ctx context.Context, name string, arguments json.RawMessage, progressToken json.RawMessage, progress ProgressCallback) (*protocol.CallToolResult, error)

	// Close releases the client's resources (process, sockets).
	Close(ctx context.Context) error
}
