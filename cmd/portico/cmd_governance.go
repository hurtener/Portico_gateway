// Command portico governance: offline CLI for the Phase 15.5 governance
// entities (customers + teams). Opens the SQLite data dir directly and calls
// the GovernanceStore — same offline pattern as `portico agents` (Phase 14).
//
// Usage:
//
//	portico governance customers list|get|create|update|delete --tenant <id> [flags]
//	portico governance teams    list|get|create|update|delete --tenant <id> [flags]
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/storage"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// defaultGovDSN mirrors cmd_agents.go's default SQLite DSN.
const defaultGovDSN = "file:./data/portico.db"

// governanceBackend narrows storage.Backend to the Governance accessor.
// Phase 15.5 kept Governance() as a concrete *DB accessor (like the LLM
// stores) rather than widening ifaces.Backend, so we duck-type to it without
// importing the sqlite driver (CLAUDE.md §4.4).
type governanceBackend interface {
	Governance() ifaces.GovernanceStore
}

func runGovernance(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("governance: resource required (customers|teams)")
	}
	resource, rest := args[0], args[1:]
	switch resource {
	case "customers":
		return runGovernanceCustomers(ctx, rest)
	case "teams":
		return runGovernanceTeams(ctx, rest)
	default:
		return fmt.Errorf("governance: unknown resource %q (want customers|teams)", resource)
	}
}

// openGovernanceStore opens the SQLite data dir and returns the Governance
// store plus a closer. Mirrors openAgentStore from cmd_agents.go.
func openGovernanceStore(dsn string) (func(), ifaces.GovernanceStore, error) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	be, err := storage.Open(context.Background(), config.StorageConfig{Driver: "sqlite", DSN: dsn}, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("open storage: %w", err)
	}
	gb, ok := be.(governanceBackend)
	if !ok {
		_ = be.Close()
		return nil, nil, fmt.Errorf("governance: storage backend %T does not expose Governance()", be)
	}
	return func() { _ = be.Close() }, gb.Governance(), nil
}

func runGovernanceCustomers(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("governance customers: verb required (list|get|create|update|delete)")
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "list":
		return runGovernanceCustomersList(ctx, rest)
	case "get":
		return runGovernanceCustomersGet(ctx, rest)
	case "create":
		return runGovernanceCustomersCreate(ctx, rest)
	case "update":
		return runGovernanceCustomersUpdate(ctx, rest)
	case "delete":
		return runGovernanceCustomersDelete(ctx, rest)
	default:
		return fmt.Errorf("governance customers: unknown verb %q (want list|get|create|update|delete)", verb)
	}
}

func runGovernanceCustomersList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("governance customers list", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	dsn := fs.String("dsn", defaultGovDSN, "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("governance customers list: --tenant is required")
	}
	closeFn, store, err := openGovernanceStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()
	customers, err := store.ListCustomers(ctx, *tenant)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(customers)
}

func runGovernanceCustomersGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("governance customers get", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	id := fs.String("id", "", "customer id (required)")
	dsn := fs.String("dsn", defaultGovDSN, "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("governance customers get: --tenant is required")
	}
	if *id == "" {
		return errors.New("governance customers get: --id is required")
	}
	closeFn, store, err := openGovernanceStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()
	c, err := store.GetCustomer(ctx, *tenant, *id)
	if err != nil {
		if errors.Is(err, ifaces.ErrGovernanceNotFound) {
			return errors.New("governance customer not found")
		}
		return fmt.Errorf("get: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(c)
}

func runGovernanceCustomersCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("governance customers create", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	name := fs.String("name", "", "customer name (required)")
	description := fs.String("description", "", "customer description")
	webhookURL := fs.String("webhook-url", "", "customer webhook URL")
	dsn := fs.String("dsn", defaultGovDSN, "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("governance customers create: --tenant is required")
	}
	if *name == "" {
		return errors.New("governance customers create: --name is required")
	}
	closeFn, store, err := openGovernanceStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()
	c := &ifaces.Customer{
		TenantID:    *tenant,
		ID:          "cust_" + randHex16(),
		Name:        *name,
		Description: *description,
		WebhookURL:  *webhookURL,
	}
	if err := store.PutCustomer(ctx, c); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	created, err := store.GetCustomer(ctx, *tenant, c.ID)
	if err != nil {
		return fmt.Errorf("get after create: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(created)
}

func runGovernanceCustomersUpdate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("governance customers update", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	id := fs.String("id", "", "customer id (required)")
	name := fs.String("name", "", "customer name")
	description := fs.String("description", "", "customer description")
	webhookURL := fs.String("webhook-url", "", "customer webhook URL")
	dsn := fs.String("dsn", defaultGovDSN, "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("governance customers update: --tenant is required")
	}
	if *id == "" {
		return errors.New("governance customers update: --id is required")
	}
	closeFn, store, err := openGovernanceStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()
	existing, err := store.GetCustomer(ctx, *tenant, *id)
	if err != nil {
		if errors.Is(err, ifaces.ErrGovernanceNotFound) {
			return errors.New("governance customer not found")
		}
		return fmt.Errorf("get: %w", err)
	}
	if *name != "" {
		existing.Name = *name
	}
	if *description != "" {
		existing.Description = *description
	}
	if *webhookURL != "" {
		existing.WebhookURL = *webhookURL
	}
	if err := store.PutCustomer(ctx, existing); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	updated, err := store.GetCustomer(ctx, *tenant, *id)
	if err != nil {
		return fmt.Errorf("get after update: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(updated)
}

func runGovernanceCustomersDelete(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("governance customers delete", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	id := fs.String("id", "", "customer id (required)")
	dsn := fs.String("dsn", defaultGovDSN, "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("governance customers delete: --tenant is required")
	}
	if *id == "" {
		return errors.New("governance customers delete: --id is required")
	}
	closeFn, store, err := openGovernanceStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()
	if err := store.DeleteCustomer(ctx, *tenant, *id); err != nil {
		if errors.Is(err, ifaces.ErrGovernanceNotFound) {
			return errors.New("governance customer not found")
		}
		return fmt.Errorf("delete: %w", err)
	}
	fmt.Fprintf(os.Stdout, "deleted %s\n", *id)
	return nil
}

func runGovernanceTeams(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("governance teams: verb required (list|get|create|update|delete)")
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "list":
		return runGovernanceTeamsList(ctx, rest)
	case "get":
		return runGovernanceTeamsGet(ctx, rest)
	case "create":
		return runGovernanceTeamsCreate(ctx, rest)
	case "update":
		return runGovernanceTeamsUpdate(ctx, rest)
	case "delete":
		return runGovernanceTeamsDelete(ctx, rest)
	default:
		return fmt.Errorf("governance teams: unknown verb %q (want list|get|create|update|delete)", verb)
	}
}

func runGovernanceTeamsList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("governance teams list", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	dsn := fs.String("dsn", defaultGovDSN, "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("governance teams list: --tenant is required")
	}
	closeFn, store, err := openGovernanceStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()
	teams, err := store.ListTeams(ctx, *tenant)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(teams)
}

func runGovernanceTeamsGet(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("governance teams get", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	id := fs.String("id", "", "team id (required)")
	dsn := fs.String("dsn", defaultGovDSN, "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("governance teams get: --tenant is required")
	}
	if *id == "" {
		return errors.New("governance teams get: --id is required")
	}
	closeFn, store, err := openGovernanceStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()
	tm, err := store.GetTeam(ctx, *tenant, *id)
	if err != nil {
		if errors.Is(err, ifaces.ErrGovernanceNotFound) {
			return errors.New("governance team not found")
		}
		return fmt.Errorf("get: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(tm)
}

func runGovernanceTeamsCreate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("governance teams create", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	name := fs.String("name", "", "team name (required)")
	customerID := fs.String("customer-id", "", "parent customer id (optional)")
	description := fs.String("description", "", "team description")
	dsn := fs.String("dsn", defaultGovDSN, "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("governance teams create: --tenant is required")
	}
	if *name == "" {
		return errors.New("governance teams create: --name is required")
	}
	closeFn, store, err := openGovernanceStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()
	tm := &ifaces.Team{
		TenantID:    *tenant,
		ID:          "team_" + randHex16(),
		CustomerID:  *customerID,
		Name:        *name,
		Description: *description,
	}
	if err := store.PutTeam(ctx, tm); err != nil {
		return fmt.Errorf("create: %w", err)
	}
	created, err := store.GetTeam(ctx, *tenant, tm.ID)
	if err != nil {
		return fmt.Errorf("get after create: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(created)
}

func runGovernanceTeamsUpdate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("governance teams update", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	id := fs.String("id", "", "team id (required)")
	name := fs.String("name", "", "team name")
	customerID := fs.String("customer-id", "", "parent customer id")
	description := fs.String("description", "", "team description")
	dsn := fs.String("dsn", defaultGovDSN, "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("governance teams update: --tenant is required")
	}
	if *id == "" {
		return errors.New("governance teams update: --id is required")
	}
	closeFn, store, err := openGovernanceStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()
	existing, err := store.GetTeam(ctx, *tenant, *id)
	if err != nil {
		if errors.Is(err, ifaces.ErrGovernanceNotFound) {
			return errors.New("governance team not found")
		}
		return fmt.Errorf("get: %w", err)
	}
	if *name != "" {
		existing.Name = *name
	}
	if *customerID != "" {
		existing.CustomerID = *customerID
	}
	if *description != "" {
		existing.Description = *description
	}
	if err := store.PutTeam(ctx, existing); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	updated, err := store.GetTeam(ctx, *tenant, *id)
	if err != nil {
		return fmt.Errorf("get after update: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(updated)
}

func runGovernanceTeamsDelete(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("governance teams delete", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant id (required)")
	id := fs.String("id", "", "team id (required)")
	dsn := fs.String("dsn", defaultGovDSN, "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return errors.New("governance teams delete: --tenant is required")
	}
	if *id == "" {
		return errors.New("governance teams delete: --id is required")
	}
	closeFn, store, err := openGovernanceStore(*dsn)
	if err != nil {
		return err
	}
	defer closeFn()
	if err := store.DeleteTeam(ctx, *tenant, *id); err != nil {
		if errors.Is(err, ifaces.ErrGovernanceNotFound) {
			return errors.New("governance team not found")
		}
		return fmt.Errorf("delete: %w", err)
	}
	fmt.Fprintf(os.Stdout, "deleted %s\n", *id)
	return nil
}
