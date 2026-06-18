package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/mcp/codemode/catalog"
	"github.com/hurtener/Portico_gateway/internal/mcp/codemode/runtime"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// runCodeMode dispatches the `portico code-mode <render|exec>` subcommands.
// Both operate OFFLINE against a SQLite data dir (no Portico boot) — render is
// read-only; exec runs a snippet through the hardened sandbox with tool calls
// disabled (a running server is required to dispatch tools), so it tests snippet
// safety + pure computation, and records an audit event. These are local
// operator tools; admin-scope gating is enforced at the REST/MCP surface.
func runCodeMode(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("code-mode: subcommand required (render|exec)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "render":
		return runCodeModeRender(ctx, rest)
	case "exec":
		return runCodeModeExec(ctx, rest)
	default:
		return fmt.Errorf("code-mode: unknown subcommand %q (want render|exec)", sub)
	}
}

// runCodeModeRender dumps the projected .pyi stub file system for a session's
// snapshot — useful for debugging policy or sharing the catalog with an external
// consumer. Deterministic: same snapshot → byte-identical output.
func runCodeModeRender(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("code-mode render", flag.ContinueOnError)
	session := fs.String("session", "", "session id whose snapshot to render (required)")
	tenant := fs.String("tenant", "", "tenant id (optional filter)")
	level := fs.String("binding-level", "server", "stub granularity: server|tool")
	file := fs.String("file", "", "render only this virtual path (e.g. servers/github.pyi)")
	dsn := fs.String("dsn", "file:./data/portico.db?mode=ro", "SQLite DSN (read-only by default)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *session == "" {
		return errors.New("code-mode render: --session is required")
	}

	db, err := sql.Open("sqlite", *dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	snap, err := loadSessionSnapshot(db, *session, *tenant)
	if err != nil {
		return err
	}
	proj := catalog.Project(snap, bindingLevel(*level))

	if *file != "" {
		content, ok := proj.Files[*file]
		if !ok {
			return fmt.Errorf("code-mode render: no such file %q", *file)
		}
		fmt.Fprint(os.Stdout, content)
		return nil
	}

	paths := make([]string, 0, len(proj.Files))
	for p := range proj.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		fmt.Fprintf(os.Stdout, "==> %s\n%s\n", p, proj.Files[p])
	}
	return nil
}

// runCodeModeExec runs a Starlark snippet through the hardened sandbox against a
// session's snapshot, offline. Tool calls fail closed (a running server is
// required to dispatch them), so this validates snippet safety + pure compute.
// It records a code_mode.cli_exec audit event with the snippet digest.
func runCodeModeExec(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("code-mode exec", flag.ContinueOnError)
	session := fs.String("session", "", "session id whose snapshot to bind (required)")
	tenant := fs.String("tenant", "", "tenant id (optional filter)")
	level := fs.String("binding-level", "server", "stub granularity: server|tool")
	code := fs.String("code", "", "Starlark source, inline or @path/to/file.star (required)")
	dsn := fs.String("dsn", "file:./data/portico.db", "SQLite DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *session == "" {
		return errors.New("code-mode exec: --session is required")
	}
	src, err := readCodeArg(*code)
	if err != nil {
		return err
	}

	db, err := sql.Open("sqlite", *dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	snap, err := loadSessionSnapshot(db, *session, *tenant)
	if err != nil {
		return err
	}
	proj := catalog.Project(snap, bindingLevel(*level))

	res, runErr := runtime.Execute(context.Background(), src, runtime.Config{
		Bindings:   toCLIBindings(proj.Tools),
		Dispatcher: offlineDispatcher{},
	})

	status := "completed"
	if runErr != nil {
		status = "failed"
	}
	// Best-effort audit: never fail the command because audit insert failed.
	if aerr := insertCodeModeAudit(db, snap.TenantID, *session, sha256Hex(src), status); aerr != nil {
		fmt.Fprintf(os.Stderr, "warning: audit insert failed: %v\n", aerr)
	}

	if runErr != nil {
		var se *runtime.SandboxError
		if errors.As(runErr, &se) {
			return fmt.Errorf("%s (%s)", se.Code, se.Detail)
		}
		return runErr
	}
	out := map[string]any{
		"result":           res.Result,
		"output":           res.Output,
		"tool_calls":       res.ToolCalls,
		"steps":            res.Steps,
		"output_truncated": res.OutputTruncated,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// offlineDispatcher fails every in-sandbox tool call: the CLI is offline and the
// governed dispatch path (tenant/policy/approval/vault) only exists in a running
// server. Pure-compute and safety-gate testing still work.
type offlineDispatcher struct{}

func (offlineDispatcher) DispatchToolCall(_ context.Context, name string, _ json.RawMessage) (json.RawMessage, *protocol.Error) {
	return nil, protocol.NewError(protocol.ErrUpstreamUnavailable,
		"code-mode exec is offline; tool calls require a running server", map[string]string{"tool": name})
}

// loadSessionSnapshot loads the most recent catalog snapshot for a session
// (optionally tenant-scoped) and unmarshals it. payload_json is a marshalled
// snapshots.Snapshot (see internal/catalog/snapshots/store_adapter.go).
func loadSessionSnapshot(db *sql.DB, sessionID, tenantID string) (*snapshots.Snapshot, error) {
	var (
		row *sql.Row
		ctx = context.Background()
	)
	if tenantID == "" {
		row = db.QueryRowContext(ctx, `SELECT payload_json FROM catalog_snapshots WHERE session_id = ? ORDER BY created_at DESC LIMIT 1`, sessionID)
	} else {
		row = db.QueryRowContext(ctx, `SELECT payload_json FROM catalog_snapshots WHERE session_id = ? AND tenant_id = ? ORDER BY created_at DESC LIMIT 1`, sessionID, tenantID)
	}
	var payload string
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("no snapshot found for session %q", sessionID)
		}
		return nil, fmt.Errorf("load snapshot: %w", err)
	}
	var snap snapshots.Snapshot
	if err := json.Unmarshal([]byte(payload), &snap); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	return &snap, nil
}

// insertCodeModeAudit records a code_mode.cli_exec event. Mirrors the
// audit_events column set; the payload carries only a digest + status.
func insertCodeModeAudit(db *sql.DB, tenantID, sessionID, snippetSHA, status string) error {
	if tenantID == "" {
		tenantID = "unknown"
	}
	payload, _ := json.Marshal(map[string]string{"snippet_sha": snippetSHA, "status": status, "via": "cli"})
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO audit_events(id, tenant_id, type, session_id, user_id, occurred_at, payload_json)
		VALUES (?, ?, 'code_mode.cli_exec', ?, 'cli', ?, ?)`,
		newAuditID(), tenantID, sessionID, time.Now().UTC().Format(time.RFC3339), string(payload))
	return err
}

func toCLIBindings(refs []catalog.ToolRef) []runtime.ToolBinding {
	out := make([]runtime.ToolBinding, 0, len(refs))
	for _, r := range refs {
		out = append(out, runtime.ToolBinding{Module: r.Module, Func: r.Func, NamespacedName: r.Namespaced})
	}
	return out
}

func bindingLevel(s string) catalog.BindingLevel {
	if s == string(catalog.BindingTool) {
		return catalog.BindingTool
	}
	return catalog.BindingServer
}

// readCodeArg returns the snippet: a literal, or the file contents when prefixed
// with '@'.
func readCodeArg(code string) (string, error) {
	if code == "" {
		return "", errors.New("code-mode exec: --code is required (inline or @file)")
	}
	if strings.HasPrefix(code, "@") {
		b, err := os.ReadFile(code[1:])
		if err != nil {
			return "", fmt.Errorf("read code file: %w", err)
		}
		return string(b), nil
	}
	return code, nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func newAuditID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "cli-" + time.Now().UTC().Format("20060102150405.000")
	}
	return hex.EncodeToString(b[:])
}
