// Package southbound is the protocol Portico speaks outward to downstream
// A2A peers. Concrete transports live in subdirectories (http); callers
// depend on the Client interface, not the concrete type (§4.4).
package southbound

import (
	"context"
	"encoding/json"

	a2a "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
)

// Client is the surface Portico uses to talk to a single downstream A2A
// peer. One Client per peer; connection pooling, (tenant, peer) caching,
// and credential plumbing are layered on top in a separate manager.
type Client interface {
	// FetchAgentCard GETs the peer's agent card from cardURL (the peer's
	// well-known agent-card URL) and decodes it. Not a JSON-RPC call:
	// agent-card discovery is a plain HTTP GET against the peer's
	// /.well-known path.
	FetchAgentCard(ctx context.Context, cardURL string) (*a2a.AgentCard, error)

	// SendMessage issues a unary message/send against the peer. The A2A
	// result may be a Task or a Message, so the raw JSON result is
	// returned for the caller to decode against the expected shape.
	SendMessage(ctx context.Context, params a2a.MessageSendParams) (json.RawMessage, error)

	// GetTask issues tasks/get and returns the Task.
	GetTask(ctx context.Context, params a2a.TaskQueryParams) (*a2a.Task, error)

	// CancelTask issues tasks/cancel and returns the updated Task.
	CancelTask(ctx context.Context, params a2a.TaskIDParams) (*a2a.Task, error)

	// Close releases the client's resources. A2A HTTP is stateless per
	// call (no long-lived session like MCP), so the HTTP-backed Client
	// returns nil; transport-specific implementations may override.
	Close(ctx context.Context) error
}
