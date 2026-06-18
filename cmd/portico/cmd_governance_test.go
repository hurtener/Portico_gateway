package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/storage"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	sqlitestorage "github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

// seedGovernanceDB opens a fresh SQLite DSN and seeds the tenant row that the
// governance stores' FK constraints require. Mirrors seedAgentsDB.
func seedGovernanceDB(t *testing.T) string {
	t.Helper()
	dsn := "file:" + t.TempDir() + "/governance.db"
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	be, err := storage.Open(context.Background(), config.StorageConfig{Driver: "sqlite", DSN: dsn}, logger)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db := be.(*sqlitestorage.DB).SQL()
	if _, err := db.Exec(`INSERT INTO tenants(id, display_name, plan) VALUES ('acme','Acme','enterprise')`); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	_ = be.Close()
	return dsn
}

func TestGovernance_CustomersCreateGetListUpdateDelete(t *testing.T) {
	dsn := seedGovernanceDB(t)

	// Create
	createOut, err := captureStdout(t, func() error {
		return runGovernanceCustomersCreate(context.Background(), []string{
			"--tenant", "acme", "--name", "Acme Corp",
			"--description", "big customer", "--webhook-url", "https://hooks.example/acme",
			"--dsn", dsn,
		})
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var created ifaces.Customer
	if err := json.Unmarshal([]byte(createOut), &created); err != nil {
		t.Fatalf("unmarshal create: %v (%s)", err, createOut)
	}
	if created.ID == "" || !strings.HasPrefix(created.ID, "cust_") {
		t.Fatalf("create: bad id %q", created.ID)
	}
	if created.Name != "Acme Corp" || created.Description != "big customer" || created.WebhookURL != "https://hooks.example/acme" {
		t.Fatalf("create: field mismatch: %+v", created)
	}
	custID := created.ID

	// Get
	getOut, err := captureStdout(t, func() error {
		return runGovernanceCustomersGet(context.Background(), []string{"--tenant", "acme", "--id", custID, "--dsn", dsn})
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(getOut, "Acme Corp") {
		t.Errorf("get output missing name: %s", getOut)
	}

	// List
	listOut, err := captureStdout(t, func() error {
		return runGovernanceCustomersList(context.Background(), []string{"--tenant", "acme", "--dsn", dsn})
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listOut, custID) {
		t.Errorf("list output missing id: %s", listOut)
	}

	// Update
	updateOut, err := captureStdout(t, func() error {
		return runGovernanceCustomersUpdate(context.Background(), []string{
			"--tenant", "acme", "--id", custID, "--name", "Acme Renamed",
			"--dsn", dsn,
		})
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	var updated ifaces.Customer
	if err := json.Unmarshal([]byte(updateOut), &updated); err != nil {
		t.Fatalf("unmarshal update: %v (%s)", err, updateOut)
	}
	if updated.Name != "Acme Renamed" {
		t.Errorf("update: name not applied: %+v", updated)
	}
	if updated.Description != "big customer" {
		t.Errorf("update: description not preserved: %+v", updated)
	}

	// Delete
	delOut, err := captureStdout(t, func() error {
		return runGovernanceCustomersDelete(context.Background(), []string{"--tenant", "acme", "--id", custID, "--dsn", dsn})
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(delOut, "deleted "+custID) {
		t.Errorf("delete output mismatch: %s", delOut)
	}

	// Get after delete -> not found
	err = runGovernanceCustomersGet(context.Background(), []string{"--tenant", "acme", "--id", custID, "--dsn", dsn})
	if err == nil {
		t.Fatal("expected error after delete")
	}
	if !strings.Contains(err.Error(), "governance customer not found") {
		t.Errorf("wrong error after delete: %v", err)
	}
}

func TestGovernance_TeamsCreateGetListWithCustomerID(t *testing.T) {
	dsn := seedGovernanceDB(t)

	// Seed a customer to attach the team to.
	custOut, err := captureStdout(t, func() error {
		return runGovernanceCustomersCreate(context.Background(), []string{
			"--tenant", "acme", "--name", "Parent", "--dsn", dsn,
		})
	})
	if err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	var cust ifaces.Customer
	if err := json.Unmarshal([]byte(custOut), &cust); err != nil {
		t.Fatalf("unmarshal customer: %v", err)
	}

	// Create team with --customer-id
	createOut, err := captureStdout(t, func() error {
		return runGovernanceTeamsCreate(context.Background(), []string{
			"--tenant", "acme", "--name", "Marketing",
			"--customer-id", cust.ID, "--description", "growth team",
			"--dsn", dsn,
		})
	})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	var team ifaces.Team
	if err := json.Unmarshal([]byte(createOut), &team); err != nil {
		t.Fatalf("unmarshal team: %v (%s)", err, createOut)
	}
	if team.ID == "" || !strings.HasPrefix(team.ID, "team_") {
		t.Fatalf("create team: bad id %q", team.ID)
	}
	if team.CustomerID != cust.ID {
		t.Fatalf("create team: customer id mismatch: %q != %q", team.CustomerID, cust.ID)
	}
	teamID := team.ID

	// Get
	getOut, err := captureStdout(t, func() error {
		return runGovernanceTeamsGet(context.Background(), []string{"--tenant", "acme", "--id", teamID, "--dsn", dsn})
	})
	if err != nil {
		t.Fatalf("get team: %v", err)
	}
	if !strings.Contains(getOut, "Marketing") || !strings.Contains(getOut, cust.ID) {
		t.Errorf("get team output mismatch: %s", getOut)
	}

	// List
	listOut, err := captureStdout(t, func() error {
		return runGovernanceTeamsList(context.Background(), []string{"--tenant", "acme", "--dsn", dsn})
	})
	if err != nil {
		t.Fatalf("list teams: %v", err)
	}
	if !strings.Contains(listOut, teamID) {
		t.Errorf("list teams output missing id: %s", listOut)
	}

	// Delete team
	delOut, err := captureStdout(t, func() error {
		return runGovernanceTeamsDelete(context.Background(), []string{"--tenant", "acme", "--id", teamID, "--dsn", dsn})
	})
	if err != nil {
		t.Fatalf("delete team: %v", err)
	}
	if !strings.Contains(delOut, "deleted "+teamID) {
		t.Errorf("delete team output mismatch: %s", delOut)
	}
}

func TestGovernance_RequiresFlags(t *testing.T) {
	dsn := seedGovernanceDB(t)

	// customers create without --name
	if err := runGovernanceCustomersCreate(context.Background(), []string{"--tenant", "acme", "--dsn", dsn}); err == nil {
		t.Fatal("expected error for customers create without --name")
	}

	// customers get without --id
	if err := runGovernanceCustomersGet(context.Background(), []string{"--tenant", "acme", "--dsn", dsn}); err == nil {
		t.Fatal("expected error for customers get without --id")
	}

	// teams create without --name
	if err := runGovernanceTeamsCreate(context.Background(), []string{"--tenant", "acme", "--dsn", dsn}); err == nil {
		t.Fatal("expected error for teams create without --name")
	}

	// list without --tenant
	if err := runGovernanceCustomersList(context.Background(), []string{"--dsn", dsn}); err == nil {
		t.Fatal("expected error for customers list without --tenant")
	}
}

func TestGovernance_Dispatcher(t *testing.T) {
	// No resource
	if err := runGovernance(context.Background(), nil); err == nil {
		t.Fatal("expected error without resource")
	}
	// Unknown resource
	if err := runGovernance(context.Background(), []string{"widgets"}); err == nil {
		t.Fatal("expected error for unknown resource")
	}
	// customers without verb
	if err := runGovernance(context.Background(), []string{"customers"}); err == nil {
		t.Fatal("expected error without verb")
	}
}
