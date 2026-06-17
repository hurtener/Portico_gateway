package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type llmSessionStore struct {
	db *sql.DB
}

func (s *llmSessionStore) CreateSession(ctx context.Context, sess *ifaces.LLMChatSession) error {
	if sess == nil {
		return errors.New("sqlite: nil session")
	}
	if sess.TenantID == "" || sess.ChatID == "" {
		return errors.New("sqlite: session requires tenant_id and chat_id")
	}
	if sess.Alias == "" {
		return errors.New("sqlite: session requires alias")
	}
	if sess.StartedAt == "" {
		sess.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tenant_llm_sessions(
			tenant_id, chat_id, user_id, alias, started_at, ended_at, summary
		) VALUES (?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), NULLIF(?, ''))
	`,
		sess.TenantID, sess.ChatID, sess.UserID, sess.Alias,
		sess.StartedAt, sess.EndedAt, sess.Summary,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create session: %w", err)
	}
	return nil
}

func (s *llmSessionStore) GetSession(ctx context.Context, tenantID, chatID string) (*ifaces.LLMChatSession, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, chat_id, user_id, alias, started_at, ended_at, summary
		FROM tenant_llm_sessions
		WHERE tenant_id = ? AND chat_id = ?
	`, tenantID, chatID)
	return scanLLMChatSession(row)
}

func (s *llmSessionStore) ListSessions(ctx context.Context, tenantID string, limit int) ([]*ifaces.LLMChatSession, error) {
	if tenantID == "" {
		return nil, errors.New("sqlite: list sessions requires tenant_id")
	}
	query := `
		SELECT tenant_id, chat_id, user_id, alias, started_at, ended_at, summary
		FROM tenant_llm_sessions
		WHERE tenant_id = ?
		ORDER BY started_at DESC
	`
	args := []any{tenantID}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list sessions: %w", err)
	}
	defer rows.Close()
	out := make([]*ifaces.LLMChatSession, 0)
	for rows.Next() {
		sess, err := scanLLMChatSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *llmSessionStore) EndSession(ctx context.Context, tenantID, chatID, summary string) error {
	if tenantID == "" || chatID == "" {
		return errors.New("sqlite: end session requires tenant_id and chat_id")
	}
	endedAt := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
		UPDATE tenant_llm_sessions
		SET ended_at = ?, summary = NULLIF(?, '')
		WHERE tenant_id = ? AND chat_id = ?
	`, endedAt, summary, tenantID, chatID)
	if err != nil {
		return fmt.Errorf("sqlite: end session: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ifaces.ErrLLMSessionNotFound
	}
	return nil
}

func (s *llmSessionStore) AppendMessage(ctx context.Context, m *ifaces.LLMChatMessage) error {
	if m == nil {
		return errors.New("sqlite: nil message")
	}
	if m.TenantID == "" || m.ChatID == "" {
		return errors.New("sqlite: message requires tenant_id and chat_id")
	}
	if m.Role == "" {
		return errors.New("sqlite: message requires role")
	}
	if m.ContentJSON == "" {
		return errors.New("sqlite: message requires content_json")
	}
	createdAt := time.Now().UTC().Format(time.RFC3339)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: append message begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Insert with computed seq
	_, err = tx.ExecContext(ctx, `
		INSERT INTO tenant_llm_messages(
			tenant_id, chat_id, seq, role, content_json, tool_call_id, span_id, created_at
		) VALUES (?, ?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM tenant_llm_messages WHERE tenant_id = ? AND chat_id = ?), ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?)
	`,
		m.TenantID, m.ChatID, m.TenantID, m.ChatID,
		m.Role, m.ContentJSON, m.ToolCallID, m.SpanID, createdAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite: append message insert: %w", err)
	}

	// Read back the assigned seq
	var seq int
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(seq), 0) FROM tenant_llm_messages WHERE tenant_id = ? AND chat_id = ?
	`, m.TenantID, m.ChatID).Scan(&seq)
	if err != nil {
		return fmt.Errorf("sqlite: append message read seq: %w", err)
	}
	m.Seq = seq
	m.CreatedAt = createdAt

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: append message commit: %w", err)
	}
	return nil
}

func (s *llmSessionStore) ListMessages(ctx context.Context, tenantID, chatID string) ([]*ifaces.LLMChatMessage, error) {
	if tenantID == "" || chatID == "" {
		return nil, errors.New("sqlite: list messages requires tenant_id and chat_id")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, chat_id, seq, role, content_json, tool_call_id, span_id, created_at
		FROM tenant_llm_messages
		WHERE tenant_id = ? AND chat_id = ?
		ORDER BY seq ASC
	`, tenantID, chatID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list messages: %w", err)
	}
	defer rows.Close()
	out := make([]*ifaces.LLMChatMessage, 0)
	for rows.Next() {
		msg, err := scanLLMChatMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

func scanLLMChatSession(s llmScanner) (*ifaces.LLMChatSession, error) {
	var (
		sess    ifaces.LLMChatSession
		userID  sql.NullString
		endedAt sql.NullString
		summary sql.NullString
	)
	if err := s.Scan(&sess.TenantID, &sess.ChatID, &userID, &sess.Alias, &sess.StartedAt, &endedAt, &summary); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ifaces.ErrLLMSessionNotFound
		}
		return nil, fmt.Errorf("sqlite: scan session: %w", err)
	}
	sess.UserID = userID.String
	sess.EndedAt = endedAt.String
	sess.Summary = summary.String
	return &sess, nil
}

func scanLLMChatMessage(s llmScanner) (*ifaces.LLMChatMessage, error) {
	var (
		msg        ifaces.LLMChatMessage
		toolCallID sql.NullString
		spanID     sql.NullString
	)
	if err := s.Scan(&msg.TenantID, &msg.ChatID, &msg.Seq, &msg.Role, &msg.ContentJSON, &toolCallID, &spanID, &msg.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("sqlite: scan message: %w", err)
		}
		return nil, fmt.Errorf("sqlite: scan message: %w", err)
	}
	msg.ToolCallID = toolCallID.String
	msg.SpanID = spanID.String
	return &msg, nil
}
