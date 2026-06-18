package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// codeModeStore is the SQLite-backed ifaces.CodeModeStore. Every statement is
// parameterised and tenant-scoped (§6/§9).
type codeModeStore struct {
	db *sql.DB
}

const defaultCodeModeListLimit = 100

func (s *codeModeStore) PutExecution(ctx context.Context, e *ifaces.CodeModeExecution) error {
	if e == nil {
		return errors.New("sqlite: nil execution")
	}
	if e.TenantID == "" || e.ExecutionID == "" || e.SessionID == "" {
		return errors.New("sqlite: execution requires tenant_id, execution_id, session_id")
	}
	if e.Status == "" {
		return errors.New("sqlite: execution requires status")
	}
	if e.StartedAt == "" {
		e.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if e.SpanID == "" {
		e.SpanID = e.ExecutionID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO code_mode_executions(
			tenant_id, execution_id, session_id, started_at, finished_at,
			status, snippet_sha, tool_calls, tokens_saved_est, output_redacted, span_id
		) VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, NULLIF(?, ''), ?)
		ON CONFLICT(tenant_id, execution_id) DO UPDATE SET
			finished_at      = excluded.finished_at,
			status           = excluded.status,
			tool_calls       = excluded.tool_calls,
			tokens_saved_est = excluded.tokens_saved_est,
			output_redacted  = excluded.output_redacted
	`,
		e.TenantID, e.ExecutionID, e.SessionID, e.StartedAt, e.FinishedAt,
		e.Status, e.SnippetSHA, e.ToolCalls, e.TokensSavedEst, e.OutputRedacted, e.SpanID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: put code mode execution: %w", err)
	}
	return nil
}

func (s *codeModeStore) UpdateExecutionStatus(ctx context.Context, e *ifaces.CodeModeExecution) error {
	if e == nil || e.TenantID == "" || e.ExecutionID == "" {
		return errors.New("sqlite: update execution requires tenant_id and execution_id")
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE code_mode_executions
		SET status = ?, finished_at = NULLIF(?, ''), tool_calls = ?,
		    tokens_saved_est = ?, output_redacted = NULLIF(?, '')
		WHERE tenant_id = ? AND execution_id = ?
	`,
		e.Status, e.FinishedAt, e.ToolCalls, e.TokensSavedEst, e.OutputRedacted,
		e.TenantID, e.ExecutionID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: update code mode execution: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("sqlite: update code mode execution: no row for %s/%s", e.TenantID, e.ExecutionID)
	}
	return nil
}

func (s *codeModeStore) ListExecutions(ctx context.Context, tenantID, sessionID string, limit int) ([]*ifaces.CodeModeExecution, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: list executions requires tenant_id")
	}
	if limit <= 0 {
		limit = defaultCodeModeListLimit
	}
	var (
		rows *sql.Rows
		err  error
	)
	if sessionID == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT tenant_id, execution_id, session_id, started_at, finished_at,
			       status, snippet_sha, tool_calls, tokens_saved_est, output_redacted, span_id
			FROM code_mode_executions
			WHERE tenant_id = ?
			ORDER BY started_at DESC, execution_id DESC
			LIMIT ?
		`, tenantID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT tenant_id, execution_id, session_id, started_at, finished_at,
			       status, snippet_sha, tool_calls, tokens_saved_est, output_redacted, span_id
			FROM code_mode_executions
			WHERE tenant_id = ? AND session_id = ?
			ORDER BY started_at DESC, execution_id DESC
			LIMIT ?
		`, tenantID, sessionID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: list code mode executions: %w", err)
	}
	defer rows.Close()

	out := []*ifaces.CodeModeExecution{}
	for rows.Next() {
		var (
			e          ifaces.CodeModeExecution
			finishedAt sql.NullString
			output     sql.NullString
		)
		if err := rows.Scan(
			&e.TenantID, &e.ExecutionID, &e.SessionID, &e.StartedAt, &finishedAt,
			&e.Status, &e.SnippetSHA, &e.ToolCalls, &e.TokensSavedEst, &output, &e.SpanID,
		); err != nil {
			return nil, fmt.Errorf("sqlite: scan code mode execution: %w", err)
		}
		e.FinishedAt = finishedAt.String
		e.OutputRedacted = output.String
		out = append(out, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate code mode executions: %w", err)
	}
	return out, nil
}

func (s *codeModeStore) SummarizeExecutions(ctx context.Context, tenantID, since string) (*ifaces.CodeModeSummary, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: summarize executions requires tenant_id")
	}
	var (
		rows *sql.Rows
		err  error
	)
	const base = `
		SELECT status, COUNT(*), COALESCE(SUM(tool_calls), 0), COALESCE(SUM(tokens_saved_est), 0)
		FROM code_mode_executions
		WHERE tenant_id = ?`
	if since == "" {
		rows, err = s.db.QueryContext(ctx, base+" GROUP BY status", tenantID)
	} else {
		rows, err = s.db.QueryContext(ctx, base+" AND started_at >= ? GROUP BY status", tenantID, since)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: summarize code mode executions: %w", err)
	}
	defer rows.Close()

	out := &ifaces.CodeModeSummary{ByStatus: map[string]int{}}
	for rows.Next() {
		var (
			status string
			count  int
			calls  int
			saved  int
		)
		if err := rows.Scan(&status, &count, &calls, &saved); err != nil {
			return nil, fmt.Errorf("sqlite: scan code mode summary: %w", err)
		}
		out.Executions += count
		out.ToolCalls += calls
		out.TokensSavedEst += saved
		out.ByStatus[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate code mode summary: %w", err)
	}
	return out, nil
}

func (s *codeModeStore) PutContinuation(ctx context.Context, c *ifaces.CodeModeContinuation) error {
	if c == nil {
		return errors.New("sqlite: nil continuation")
	}
	if c.TenantID == "" || c.ContinuationToken == "" || c.ExecutionID == "" || c.SessionID == "" {
		return errors.New("sqlite: continuation requires tenant_id, token, execution_id, session_id")
	}
	if c.SnapshotID == "" {
		return errors.New("sqlite: continuation requires snapshot_id")
	}
	if c.AwaitingApprovalID == "" {
		return errors.New("sqlite: continuation requires awaiting_approval_id")
	}
	now := time.Now().UTC()
	if c.CreatedAt == "" {
		c.CreatedAt = now.Format(time.RFC3339)
	}
	if c.ExpiresAt == "" {
		c.ExpiresAt = now.Add(defaultContinuationTTL).Format(time.RFC3339)
	}
	if c.CachedResultsJSON == "" {
		c.CachedResultsJSON = "[]"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO code_mode_continuations(
			tenant_id, continuation_token, execution_id, session_id, snapshot_id,
			code, cached_results, awaiting_call_index, awaiting_approval_id,
			print_buffer, clock_unix, created_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		c.TenantID, c.ContinuationToken, c.ExecutionID, c.SessionID, c.SnapshotID,
		c.Code, c.CachedResultsJSON, c.AwaitingCallIndex, c.AwaitingApprovalID,
		c.PrintBuffer, c.ClockUnix, c.CreatedAt, c.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite: put code mode continuation: %w", err)
	}
	return nil
}

// defaultContinuationTTL bounds how long a suspended execution can wait for
// approval before resume fails closed (plan: 24h, configurable).
const defaultContinuationTTL = 24 * time.Hour

func (s *codeModeStore) ConsumeContinuation(ctx context.Context, tenantID, token string, now time.Time) (*ifaces.CodeModeContinuation, error) {
	if tenantID == "" || token == "" {
		return nil, ifaces.ErrContinuationNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("sqlite: consume continuation: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	c, err := scanContinuation(tx.QueryRowContext(ctx, `
		SELECT tenant_id, continuation_token, execution_id, session_id, snapshot_id,
		       code, cached_results, awaiting_call_index, awaiting_approval_id,
		       print_buffer, clock_unix, created_at, expires_at, consumed_at
		FROM code_mode_continuations
		WHERE tenant_id = ? AND continuation_token = ?
	`, tenantID, token))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ifaces.ErrContinuationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: consume continuation: scan: %w", err)
	}

	// Single-use: a non-empty consumed_at means a prior resume already used it.
	if c.ConsumedAt != "" {
		return nil, ifaces.ErrContinuationConsumed
	}

	// TTL: an expired token can never be resumed; delete it and report expiry.
	if expiry, perr := time.Parse(time.RFC3339, c.ExpiresAt); perr == nil && now.After(expiry) {
		if _, derr := tx.ExecContext(ctx, `
			DELETE FROM code_mode_continuations WHERE tenant_id = ? AND continuation_token = ?
		`, tenantID, token); derr != nil {
			return nil, fmt.Errorf("sqlite: consume continuation: delete expired: %w", derr)
		}
		if cerr := tx.Commit(); cerr != nil {
			return nil, fmt.Errorf("sqlite: consume continuation: commit expiry: %w", cerr)
		}
		return nil, ifaces.ErrContinuationExpired
	}

	// Mark consumed atomically. The WHERE consumed_at IS NULL guard makes a
	// concurrent double-resume race lose deterministically.
	consumedAt := now.UTC().Format(time.RFC3339)
	res, err := tx.ExecContext(ctx, `
		UPDATE code_mode_continuations SET consumed_at = ?
		WHERE tenant_id = ? AND continuation_token = ? AND consumed_at IS NULL
	`, consumedAt, tenantID, token)
	if err != nil {
		return nil, fmt.Errorf("sqlite: consume continuation: mark: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ifaces.ErrContinuationConsumed
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("sqlite: consume continuation: commit: %w", err)
	}
	c.ConsumedAt = consumedAt
	return c, nil
}

func (s *codeModeStore) DeleteExpiredContinuations(ctx context.Context, before time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM code_mode_continuations
		WHERE expires_at < ? OR consumed_at IS NOT NULL
	`, before.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("sqlite: delete expired continuations: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func scanContinuation(row interface{ Scan(...any) error }) (*ifaces.CodeModeContinuation, error) {
	var (
		c          ifaces.CodeModeContinuation
		consumedAt sql.NullString
	)
	if err := row.Scan(
		&c.TenantID, &c.ContinuationToken, &c.ExecutionID, &c.SessionID, &c.SnapshotID,
		&c.Code, &c.CachedResultsJSON, &c.AwaitingCallIndex, &c.AwaitingApprovalID,
		&c.PrintBuffer, &c.ClockUnix, &c.CreatedAt, &c.ExpiresAt, &consumedAt,
	); err != nil {
		return nil, err
	}
	c.ConsumedAt = consumedAt.String
	return &c, nil
}
