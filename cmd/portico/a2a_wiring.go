package main

import (
	"context"
	"fmt"
	"log/slog"

	a2anb "github.com/hurtener/Portico_gateway/internal/a2a/northbound/http"
	a2aproto "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
	a2asb "github.com/hurtener/Portico_gateway/internal/a2a/southbound"
	a2ahttp "github.com/hurtener/Portico_gateway/internal/a2a/southbound/http"
	a2amgr "github.com/hurtener/Portico_gateway/internal/a2a/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/secrets"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// a2aClientFactory builds the manager.ClientFactory the southbound pool uses to
// turn a registered peer into a live HTTP client. Egress auth (acceptance #6) is
// resolved here from the vault by peer.EgressAuthRef and attached as a Bearer;
// the southbound client builds fresh requests, so an inbound caller's
// Authorization is never forwarded. Keeping this in cmd/portico is what lets the
// pool itself stay vault-agnostic (§4.4).
func a2aClientFactory(vault secrets.Vault, log *slog.Logger) a2amgr.ClientFactory {
	return func(ctx context.Context, peer *ifaces.A2APeer) (a2asb.Client, error) {
		cfg := a2ahttp.Config{
			PeerID:   peer.ID,
			Endpoint: peer.Endpoint,
			Logger:   log,
		}
		if peer.EgressAuthRef != "" && vault != nil {
			tok, err := vault.Get(ctx, peer.TenantID, peer.EgressAuthRef)
			if err != nil {
				return nil, fmt.Errorf("a2a egress auth for peer %s: %w", peer.ID, err)
			}
			cfg.AuthHeader = "Bearer " + tok
		}
		return a2ahttp.New(cfg), nil
	}
}

// a2aCardProvider returns the agent card Portico advertises at
// /a2a/.well-known/agent.json. Skills aggregate from the tenant's registered
// peers once agent-card ingestion lands; for now the card advertises Portico's
// identity + protocol version + (no) capabilities.
func a2aCardProvider(version string) a2anb.CardProvider {
	return func(_ context.Context, _ string) a2aproto.AgentCard {
		return a2aproto.AgentCard{
			Name:            "Portico",
			Description:     "Portico A2A gateway",
			URL:             "/a2a",
			Version:         version,
			ProtocolVersion: a2aproto.SpecVersion,
			Capabilities:    a2aproto.AgentCapabilities{Streaming: false},
		}
	}
}
