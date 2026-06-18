package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/storage"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func runAgents(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("agents: subcommand required (list|get|create|delete|bind|unbind)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return runAgentsList(ctx, rest)
	case "get":
		return runAgentsGet(ctx, rest)
	case "create":
		return runAgentsCreate(ctx, rest)
	case "delete":
		return runAgentsDelete(ctx, rest)
	case "bind":
		return runAgentsBind(ctx, rest)
	case "unbind":
		return runAgentsUnbind(ctx, rest)
	default:
		return fmt.Errorf("agents: unknown subcommand %q (want list|get|create|delete|bind|unbind)", sub)
	}
}

func openAgentStore(dsn string) (func(), ifaces.AgentProfileStore, error) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	be, err := storage.Open(context.Background(), config.StorageConfig{Driver: "sqlite", DSN: dsn}, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("open storage: %w", err)
	}
	return func() { _ = be.Close() }, be.AgentProfiles(), nil
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func randHex16() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

func runAgentsList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agents list", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	dsn := fs.String("dsn", "file:./data/portico.db", "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("agents list: --tenant is required")
	}

	closeFn, store, err := openAgentStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()

	profiles, err := store.List(ctx, *tenant)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(profiles)
}

func runAgentsGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agents get", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	id := fs.String("id", "", "profile id (required)")
	dsn := fs.String("dsn", "file:./data/portico.db", "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("agents get: --tenant is required")
	}
	if *id == "" {
		return errors.New("agents get: --id is required")
	}

	closeFn, store, err := openAgentStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()

	profile, err := store.Get(ctx, *tenant, *id)
	if err != nil {
		if errors.Is(err, ifaces.ErrAgentProfileNotFound) {
			return errors.New("agent profile not found")
		}
		return fmt.Errorf("get: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(profile)
}

func runAgentsCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agents create", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	name := fs.String("name", "", "profile name (required)")
	servers := fs.String("servers", "", "comma-separated allowed MCP server names")
	tools := fs.String("tools", "", "comma-separated allowed namespaced tools (server.tool)")
	skills := fs.String("skills", "", "comma-separated allowed skill pack IDs")
	models := fs.String("models", "", "comma-separated allowed model aliases")
	scopes := fs.String("scopes", "mcp:call", "comma-separated scopes")
	description := fs.String("description", "", "profile description")
	dsn := fs.String("dsn", "file:./data/portico.db", "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("agents create: --tenant is required")
	}
	if *name == "" {
		return errors.New("agents create: --name is required")
	}

	closeFn, store, err := openAgentStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()

	profile := &ifaces.AgentProfile{
		TenantID:            *tenant,
		ID:                  "ap_" + randHex16(),
		Name:                *name,
		Description:         *description,
		AllowedMCPServers:   splitCSV(*servers),
		AllowedTools:        splitCSV(*tools),
		AllowedSkills:       splitCSV(*skills),
		AllowedModelAliases: splitCSV(*models),
		Scopes:              splitCSV(*scopes),
		Enabled:             true,
	}

	if err := store.Put(ctx, profile); err != nil {
		return fmt.Errorf("create: %w", err)
	}

	created, err := store.Get(ctx, *tenant, profile.ID)
	if err != nil {
		return fmt.Errorf("get after create: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(created)
}

func runAgentsDelete(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agents delete", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	id := fs.String("id", "", "profile id (required)")
	dsn := fs.String("dsn", "file:./data/portico.db", "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("agents delete: --tenant is required")
	}
	if *id == "" {
		return errors.New("agents delete: --id is required")
	}

	closeFn, store, err := openAgentStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()

	if err := store.Delete(ctx, *tenant, *id); err != nil {
		if errors.Is(err, ifaces.ErrAgentProfileNotFound) {
			return errors.New("agent profile not found")
		}
		return fmt.Errorf("delete: %w", err)
	}

	fmt.Fprintf(os.Stdout, "deleted %s\n", *id)
	return nil
}

func runAgentsBind(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agents bind", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	id := fs.String("id", "", "profile id (required)")
	sub := fs.String("sub", "", "jwt subject (required)")
	dsn := fs.String("dsn", "file:./data/portico.db", "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("agents bind: --tenant is required")
	}
	if *id == "" {
		return errors.New("agents bind: --id is required")
	}
	if *sub == "" {
		return errors.New("agents bind: --sub is required")
	}

	closeFn, store, err := openAgentStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()

	if _, err := store.Get(ctx, *tenant, *id); err != nil {
		if errors.Is(err, ifaces.ErrAgentProfileNotFound) {
			return errors.New("agent profile not found")
		}
		return fmt.Errorf("verify profile: %w", err)
	}

	if err := store.PutJWTBinding(ctx, *tenant, *sub, *id); err != nil {
		return fmt.Errorf("bind: %w", err)
	}

	fmt.Fprintf(os.Stdout, "bound %s -> %s\n", *sub, *id)
	return nil
}

func runAgentsUnbind(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agents unbind", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	sub := fs.String("sub", "", "jwt subject (required)")
	dsn := fs.String("dsn", "file:./data/portico.db", "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("agents unbind: --tenant is required")
	}
	if *sub == "" {
		return errors.New("agents unbind: --sub is required")
	}

	closeFn, store, err := openAgentStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()

	if err := store.DeleteJWTBinding(ctx, *tenant, *sub); err != nil {
		return fmt.Errorf("unbind: %w", err)
	}

	fmt.Fprintf(os.Stdout, "unbound %s\n", *sub)
	return nil
}
