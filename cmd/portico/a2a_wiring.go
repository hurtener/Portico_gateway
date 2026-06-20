package main

import (
	"context"
	"encoding/json"
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
// /a2a/.well-known/agent.json. It aggregates the discovered skills of the
// tenant's enabled, ingested peers (each skill id namespaced "peer.skill") so an
// A2A client sees the surface Portico can route to. A nil store (or no ingested
// cards) yields just Portico's identity.
func a2aCardProvider(version string, store ifaces.A2APeerStore) a2anb.CardProvider {
	return func(ctx context.Context, tenantID string) a2aproto.AgentCard {
		card := a2aproto.AgentCard{
			Name:            "Portico",
			Description:     "Portico A2A gateway",
			URL:             "/a2a",
			Version:         version,
			ProtocolVersion: a2aproto.SpecVersion,
			Capabilities:    a2aproto.AgentCapabilities{Streaming: false},
		}
		if store == nil {
			return card
		}
		peers, err := store.ListPeers(ctx, tenantID)
		if err != nil {
			return card
		}
		for _, p := range peers {
			if !p.Enabled || p.AgentCardJSON == "" {
				continue
			}
			var pc a2aproto.AgentCard
			if json.Unmarshal([]byte(p.AgentCardJSON), &pc) != nil {
				continue
			}
			for _, sk := range pc.Skills {
				card.Skills = append(card.Skills, a2aproto.AgentSkill{
					ID:          p.Name + "." + sk.ID,
					Name:        sk.Name,
					Description: sk.Description,
					Tags:        sk.Tags,
				})
			}
		}
		return card
	}
}
