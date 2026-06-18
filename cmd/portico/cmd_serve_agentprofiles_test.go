package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/storage"
)

// TestSeedAgentProfilesFromConfig_Idempotent verifies that cold-start seeding is
// idempotent: seeding the same config twice leaves exactly one profile with a
// stable id (no duplicate, despite the random id minting on first insert) and
// applies the JWT binding (Phase 14, acceptance-adjacent config #10).
func TestSeedAgentProfilesFromConfig_Idempotent(t *testing.T) {
	dsn := "file:" + t.TempDir() + "/seed.db"
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	be, err := storage.Open(context.Background(), config.StorageConfig{Driver: "sqlite", DSN: dsn}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = be.Close() }()
	store := be.AgentProfiles()
	ctx := context.Background()

	cfg := &config.Config{
		Tenants: []config.TenantConfig{{ID: "acme"}},
		AgentProfiles: []config.AgentProfileConfig{{
			Name:              "support",
			AllowedMCPServers: []string{"zendesk"},
			AllowedSkills:     []string{"triage"},
			Scopes:            []string{"mcp:call"},
			Bindings:          []string{"agent-1"},
		}},
	}

	if err := seedAgentProfilesFromConfig(ctx, store, cfg, logger); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	first, err := store.List(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 {
		t.Fatalf("after first seed: want 1 profile, got %d", len(first))
	}
	firstID := first[0].ID

	// Seed again — must not duplicate, must reuse the id.
	if err := seedAgentProfilesFromConfig(ctx, store, cfg, logger); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	second, err := store.List(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 {
		t.Fatalf("after second seed: want 1 profile (idempotent), got %d", len(second))
	}
	if second[0].ID != firstID {
		t.Fatalf("id not stable across restarts: %q != %q", second[0].ID, firstID)
	}

	// The binding resolves to the seeded profile.
	bound, err := store.ResolveJWTBinding(ctx, "acme", "agent-1")
	if err != nil {
		t.Fatalf("binding not seeded: %v", err)
	}
	if bound.ID != firstID || bound.Name != "support" {
		t.Fatalf("binding resolves to the wrong profile: %+v", bound)
	}
}

// TestSeedAgentProfilesFromConfig_DefaultsTenant verifies a single configured
// tenant is the implicit owner when a profile omits its tenant.
func TestSeedAgentProfilesFromConfig_DefaultsTenant(t *testing.T) {
	dsn := "file:" + t.TempDir() + "/seed2.db"
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	be, err := storage.Open(context.Background(), config.StorageConfig{Driver: "sqlite", DSN: dsn}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = be.Close() }()
	ctx := context.Background()

	cfg := &config.Config{
		Tenants:       []config.TenantConfig{{ID: "solo"}},
		AgentProfiles: []config.AgentProfileConfig{{Name: "p"}},
	}
	if err := seedAgentProfilesFromConfig(ctx, be.AgentProfiles(), cfg, logger); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rows, err := be.AgentProfiles().List(ctx, "solo")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("profile not seeded under the sole tenant: %d rows", len(rows))
	}
}
