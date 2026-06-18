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

func seedAgentsDB(t *testing.T) string {
	t.Helper()
	dsn := "file:" + t.TempDir() + "/agents.db"
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

func TestAgents_CreateGetListDelete(t *testing.T) {
	dsn := seedAgentsDB(t)

	// Create
	createOut, err := captureStdout(t, func() error {
		return runAgentsCreate(context.Background(), []string{"--tenant", "acme", "--name", "test-agent", "--servers", "github,jira", "--tools", "github.list_issues", "--skills", "skill-1", "--models", "gpt-4o", "--scopes", "mcp:call,llm:invoke", "--description", "Test agent", "--dsn", dsn})
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var created ifaces.AgentProfile
	if err := json.Unmarshal([]byte(createOut), &created); err != nil {
		t.Fatalf("unmarshal create output: %v", err)
	}
	if created.ID == "" {
		t.Fatal("create: missing ID")
	}
	if created.Name != "test-agent" {
		t.Fatalf("create: name mismatch: %s", created.Name)
	}
	profileID := created.ID

	// Get
	getOut, err := captureStdout(t, func() error {
		return runAgentsGet(context.Background(), []string{"--tenant", "acme", "--id", profileID, "--dsn", dsn})
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(getOut, "test-agent") {
		t.Errorf("get output missing name: %s", getOut)
	}

	// List
	listOut, err := captureStdout(t, func() error {
		return runAgentsList(context.Background(), []string{"--tenant", "acme", "--dsn", dsn})
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listOut, profileID) {
		t.Errorf("list output missing profile ID: %s", listOut)
	}

	// Delete
	delOut, err := captureStdout(t, func() error {
		return runAgentsDelete(context.Background(), []string{"--tenant", "acme", "--id", profileID, "--dsn", dsn})
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(delOut, "deleted "+profileID) {
		t.Errorf("delete output mismatch: %s", delOut)
	}
}

func TestAgents_Get_NotFound(t *testing.T) {
	dsn := seedAgentsDB(t)

	err := runAgentsGet(context.Background(), []string{"--tenant", "acme", "--id", "ap_nonexistent", "--dsn", dsn})
	if err == nil {
		t.Fatal("expected error for non-existent profile")
	}
	if !strings.Contains(err.Error(), "agent profile not found") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestAgents_BindUnbind(t *testing.T) {
	dsn := seedAgentsDB(t)

	// Create a profile first
	createOut, err := captureStdout(t, func() error {
		return runAgentsCreate(context.Background(), []string{"--tenant", "acme", "--name", "bind-test", "--dsn", dsn})
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var created ifaces.AgentProfile
	if err := json.Unmarshal([]byte(createOut), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	profileID := created.ID

	// Bind
	bindOut, err := captureStdout(t, func() error {
		return runAgentsBind(context.Background(), []string{"--tenant", "acme", "--id", profileID, "--sub", "user-123", "--dsn", dsn})
	})
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if !strings.Contains(bindOut, "bound user-123 -> "+profileID) {
		t.Errorf("bind output mismatch: %s", bindOut)
	}

	// Unbind
	unbindOut, err := captureStdout(t, func() error {
		return runAgentsUnbind(context.Background(), []string{"--tenant", "acme", "--sub", "user-123", "--dsn", dsn})
	})
	if err != nil {
		t.Fatalf("unbind: %v", err)
	}
	if !strings.Contains(unbindOut, "unbound user-123") {
		t.Errorf("unbind output mismatch: %s", unbindOut)
	}
}

func TestAgents_RequiresTenant(t *testing.T) {
	err := runAgents(context.Background(), []string{"list"})
	if err == nil {
		t.Fatal("expected error without --tenant")
	}
	if !strings.Contains(err.Error(), "--tenant is required") {
		t.Errorf("wrong error: %v", err)
	}
}
