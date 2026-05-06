package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	porticohttp "github.com/hurtener/Portico_gateway/internal/mcp/northbound/http"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/policy/approval"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// sqlOpener is implemented by every storage backend whose underlying
// driver is *sql.DB. The Phase 5 audit Store needs raw access for
// batched inserts; the canonical Backend interface intentionally keeps
// that capability backend-specific.
type sqlOpener interface {
	SQL() *sql.DB
}

// rawSQLFromBackend extracts the underlying *sql.DB from a backend that
// supports it. Returns an error when the backend is not SQL-driven (e.g.
// a future external proxy) — the audit store is then disabled and the
// SlogEmitter alone carries events.
func rawSQLFromBackend(b ifaces.Backend) (*sql.DB, error) {
	if o, ok := b.(sqlOpener); ok {
		return o.SQL(), nil
	}
	return nil, errors.New("backend does not expose *sql.DB")
}

// serverInitSenderAdapter wraps *porticohttp.ServerInitiatedRequester so
// it satisfies approval.Sender. The interface is narrower than the full
// requester surface so the approval package stays free of the http
// transport import.
type serverInitSenderAdapter struct {
	r *porticohttp.ServerInitiatedRequester
}

// Elicit ships an elicitation/create request and waits for the matching
// response.
func (a serverInitSenderAdapter) Elicit(ctx context.Context, sessionID string, params protocol.ElicitationCreateParams, timeout time.Duration) (*protocol.ElicitationCreateResult, error) {
	resp, err := a.r.Send(ctx, sessionID, protocol.MethodElicitationCreate, params, timeout)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	var out protocol.ElicitationCreateResult
	if len(resp.Result) > 0 {
		if err := unmarshalJSON(resp.Result, &out); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

// sessionLookupAdapter satisfies approval.SessionLookup with the live
// SessionRegistry.
type sessionLookupAdapter struct {
	sessions *mcpgw.SessionRegistry
}

// HasElicitation reports the client's elicitation capability (recorded
// during initialize).
func (a sessionLookupAdapter) HasElicitation(sessionID string) bool {
	if a.sessions == nil {
		return false
	}
	sess, ok := a.sessions.Get(sessionID)
	if !ok {
		return false
	}
	return sess.ClientCaps.HasElicitation
}

// unmarshalJSON is a tiny shim so the wiring file stays self-contained.
// Using encoding/json directly causes an import cycle with cmd_serve.go's
// (already-imported) json package only when the symbol is named the same
// — so we keep this tiny helper to read better at call sites.
func unmarshalJSON(b []byte, v any) error {
	return jsonUnmarshal(b, v)
}

// approvalFlowResolverFor returns a closure suitable for
// api.NewApprovalFlowAdapter — wraps Flow.ResolveManually so the api
// package can call it without importing the approval package.
func approvalFlowResolverFor(f *approval.Flow) func(ctx context.Context, tenantID, id, status, actor string) (*approval.Approval, error) {
	if f == nil {
		return nil
	}
	return f.ResolveManually
}
