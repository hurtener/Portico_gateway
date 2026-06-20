// Package ingest fetches and persists registered A2A peers' agent cards. A
// peer's card (the A2A analog of MCP's tools/list) advertises the tasks/skills
// it offers; Portico caches it on the peer row so the catalog + the northbound
// agent card can surface the discovered surface without a live round-trip.
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/hurtener/Portico_gateway/internal/a2a/southbound"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// PeerClientPool is the slice of the southbound pool the refresher needs: a
// governed, egress-authed client for a registered peer.
type PeerClientPool interface {
	Acquire(ctx context.Context, tenantID, peerID string) (southbound.Client, error)
}

// Refresher fetches a peer's agent card and persists it on the peer row.
type Refresher struct {
	store ifaces.A2APeerStore
	pool  PeerClientPool
	log   *slog.Logger
}

// NewRefresher builds a Refresher. log defaults to slog.Default().
func NewRefresher(store ifaces.A2APeerStore, pool PeerClientPool, log *slog.Logger) *Refresher {
	if log == nil {
		log = slog.Default()
	}
	return &Refresher{store: store, pool: pool, log: log}
}

// ErrCardFetch wraps a failure to fetch/decode a peer's agent card so callers
// (the REST handler) can map it to a 502.
var ErrCardFetch = errors.New("a2a: agent card fetch failed")

// RefreshCard fetches peer (tenantID, peerID)'s agent card from its well-known
// URL (derived from the peer endpoint), persists it as agent_card_json, and
// returns the updated peer. Peer lookup is tenant-scoped; ifaces.ErrA2APeerNotFound
// passes through. A fetch/decode failure is wrapped in ErrCardFetch.
func (r *Refresher) RefreshCard(ctx context.Context, tenantID, peerID string) (*ifaces.A2APeer, error) {
	peer, err := r.store.GetPeer(ctx, tenantID, peerID)
	if err != nil {
		return nil, err
	}
	client, err := r.pool.Acquire(ctx, tenantID, peerID)
	if err != nil {
		return nil, err
	}
	card, err := client.FetchAgentCard(ctx, CardURL(peer.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("%w: peer %s: %s", ErrCardFetch, peerID, err.Error())
	}
	b, err := json.Marshal(card)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal card: %s", ErrCardFetch, err.Error())
	}
	peer.AgentCardJSON = string(b)
	if err := r.store.PutPeer(ctx, peer); err != nil {
		return nil, err
	}
	r.log.Info("a2a agent card refreshed", "tenant_id", tenantID, "peer_id", peerID, "skills", len(card.Skills))
	return r.store.GetPeer(ctx, tenantID, peerID)
}

// CardURL maps a peer's JSON-RPC endpoint to its well-known agent-card URL:
// scheme://host[:port]/.well-known/agent.json (the A2A discovery convention).
// A non-URL endpoint falls back to appending the well-known path.
func CardURL(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return strings.TrimRight(endpoint, "/") + "/.well-known/agent.json"
	}
	u.Path = "/.well-known/agent.json"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
