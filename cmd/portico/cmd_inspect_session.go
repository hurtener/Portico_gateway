package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

// runInspectSession reads SQLite directly (no Portico boot) and emits
// a structured dump for offline analysis. Useful when triaging an
// incident from a snapshot of the data dir.
func runInspectSession(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("inspect-session", flag.ContinueOnError)
	output := fs.String("output", "json", "output format: json|table")
	since := fs.String("since", "", "RFC3339 lower bound for audit/drift events")
	dsn := fs.String("dsn", "file:./data/portico.db?mode=ro", "SQLite DSN (read-only by default)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("inspect-session: session_id required")
	}
	sessionID := fs.Arg(0)

	db, err := sql.Open("sqlite", *dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	dump, err := buildSessionDump(db, sessionID, *since)
	if err != nil {
		return err
	}

	switch *output {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(dump)
	case "table":
		printTable(os.Stdout, dump)
		return nil
	default:
		return fmt.Errorf("unknown --output %q", *output)
	}
}

type sessionDump struct {
	Session      sessionRow      `json:"session"`
	Snapshot     json.RawMessage `json:"snapshot,omitempty"`
	AuditEvents  []auditRow      `json:"audit_events"`
	Approvals    []approvalRow   `json:"approvals"`
	DriftEvents  []auditRow      `json:"drift_events"`
	TraceSummary traceSummary    `json:"trace_summary"`
}

type sessionRow struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	UserID     string     `json:"user_id,omitempty"`
	SnapshotID string     `json:"snapshot_id,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

type auditRow struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type approvalRow struct {
	ID        string    `json:"id"`
	Tool      string    `json:"tool"`
	RiskClass string    `json:"risk_class"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// traceSummary is best-effort — populated from audit events that carry
// trace_id. The OTLP exporter is the canonical sink for full spans;
// inspect-session works offline so we summarise what's persistable.
type traceSummary struct {
	TotalEvents int       `json:"total_events"`
	Errors      int       `json:"errors"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	EndedAt     time.Time `json:"ended_at,omitempty"`
}

const sqliteFmt = "2006-01-02T15:04:05.000Z"

// buildSessionDump assembles the offline view of a session. The function
// is a sequence of independent SQL reads (session row → snapshot payload
// → audit events → approvals → trace summary). Splitting the branches
// would obscure the column projection the operator reads top-to-bottom.
//
//nolint:gocyclo
func buildSessionDump(db *sql.DB, sessionID, sinceRFC string) (*sessionDump, error) {
	ctx := context.Background()
	out := &sessionDump{}

	// Session row.
	row := db.QueryRowContext(ctx, `
		SELECT id, tenant_id, COALESCE(user_id, ''), COALESCE(snapshot_id, ''), started_at, ended_at
		FROM sessions WHERE id = ?
	`, sessionID)
	var (
		s        sessionRow
		startStr string
		endedStr sql.NullString
	)
	if err := row.Scan(&s.ID, &s.TenantID, &s.UserID, &s.SnapshotID, &startStr, &endedStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("session %q not found", sessionID)
		}
		return nil, err
	}
	if t, err := time.Parse(sqliteFmt, startStr); err == nil {
		s.StartedAt = t
	}
	if endedStr.Valid && endedStr.String != "" {
		if t, err := time.Parse(sqliteFmt, endedStr.String); err == nil {
			s.EndedAt = &t
		}
	}
	out.Session = s

	// Snapshot payload.
	if s.SnapshotID != "" {
		var body string
		if err := db.QueryRowContext(ctx, `SELECT payload_json FROM catalog_snapshots WHERE id = ?`, s.SnapshotID).Scan(&body); err == nil {
			out.Snapshot = json.RawMessage(body)
		}
	}

	// Audit events.
	since := time.Time{}
	if sinceRFC != "" {
		if t, err := time.Parse(time.RFC3339, sinceRFC); err == nil {
			since = t
		}
	}
	auditQ := `
		SELECT id, type, occurred_at, COALESCE(payload_json, '')
		FROM audit_events
		WHERE tenant_id = ? AND session_id = ?
	`
	args := []any{s.TenantID, s.ID}
	if !since.IsZero() {
		auditQ += " AND occurred_at >= ?"
		args = append(args, since.UTC().Format(sqliteFmt))
	}
	auditQ += " ORDER BY occurred_at ASC"
	rows, err := db.QueryContext(ctx, auditQ, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			r        auditRow
			occurred string
			payload  string
		)
		if err := rows.Scan(&r.ID, &r.Type, &occurred, &payload); err != nil {
			return nil, err
		}
		if t, err := time.Parse(sqliteFmt, occurred); err == nil {
			r.OccurredAt = t
		}
		if payload != "" {
			r.Payload = json.RawMessage(payload)
		}
		if r.Type == "schema.drift" {
			out.DriftEvents = append(out.DriftEvents, r)
		}
		out.AuditEvents = append(out.AuditEvents, r)
	}

	// Approvals.
	apRows, err := db.QueryContext(ctx, `
		SELECT id, tool, COALESCE(risk_class, ''), status, created_at
		FROM approvals WHERE tenant_id = ? AND session_id = ?
		ORDER BY created_at ASC
	`, s.TenantID, s.ID)
	if err == nil {
		defer apRows.Close()
		for apRows.Next() {
			var (
				a          approvalRow
				createdStr string
			)
			if err := apRows.Scan(&a.ID, &a.Tool, &a.RiskClass, &a.Status, &createdStr); err != nil {
				return nil, err
			}
			if t, err := time.Parse(sqliteFmt, createdStr); err == nil {
				a.CreatedAt = t
			}
			out.Approvals = append(out.Approvals, a)
		}
	}

	// Trace summary.
	out.TraceSummary.TotalEvents = len(out.AuditEvents)
	for _, e := range out.AuditEvents {
		if e.Type == "tool_call.failed" || e.Type == "policy.denied" {
			out.TraceSummary.Errors++
		}
	}
	if len(out.AuditEvents) > 0 {
		out.TraceSummary.StartedAt = out.AuditEvents[0].OccurredAt
		out.TraceSummary.EndedAt = out.AuditEvents[len(out.AuditEvents)-1].OccurredAt
	}
	return out, nil
}

func printTable(w io.Writer, d *sessionDump) {
	fmt.Fprintf(w, "Session: %s\n", d.Session.ID)
	fmt.Fprintf(w, "  Tenant: %s\n", d.Session.TenantID)
	fmt.Fprintf(w, "  User:   %s\n", d.Session.UserID)
	fmt.Fprintf(w, "  Snapshot: %s\n", d.Session.SnapshotID)
	fmt.Fprintf(w, "  Started: %s\n", d.Session.StartedAt.Format(time.RFC3339))
	if d.Session.EndedAt != nil {
		fmt.Fprintf(w, "  Ended:   %s\n", d.Session.EndedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(w, "\nAudit events: %d (errors: %d)\n", d.TraceSummary.TotalEvents, d.TraceSummary.Errors)
	for _, e := range d.AuditEvents {
		fmt.Fprintf(w, "  %s  %-30s\n", e.OccurredAt.Format(time.RFC3339), e.Type)
	}
	if len(d.DriftEvents) > 0 {
		fmt.Fprintf(w, "\nDrift events: %d\n", len(d.DriftEvents))
	}
	if len(d.Approvals) > 0 {
		fmt.Fprintf(w, "\nApprovals: %d\n", len(d.Approvals))
		for _, a := range d.Approvals {
			fmt.Fprintf(w, "  %s  %-12s %s\n", a.ID, a.Status, a.Tool)
		}
	}
}
