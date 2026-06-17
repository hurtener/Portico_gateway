package ifaces

import (
	"context"
	"errors"
)

// LLMChatSession is one brokered conversation.
type LLMChatSession struct {
	TenantID  string
	ChatID    string
	UserID    string // may be empty
	Alias     string
	StartedAt string
	EndedAt   string // empty while the chat is open
	Summary   string // empty until set
}

// LLMChatMessage is one message in a chat, ordered by Seq within (tenant, chat).
type LLMChatMessage struct {
	TenantID    string
	ChatID      string
	Seq         int
	Role        string
	ContentJSON string
	ToolCallID  string
	SpanID      string
	CreatedAt   string
}

// ErrLLMSessionNotFound is returned when no session row exists for (tenant, chat).
var ErrLLMSessionNotFound = errors.New("storage: llm session not found")

// LLMSessionStore persists brokered LLM chat sessions + their messages. Every
// method is tenant-scoped.
type LLMSessionStore interface {
	CreateSession(ctx context.Context, s *LLMChatSession) error
	GetSession(ctx context.Context, tenantID, chatID string) (*LLMChatSession, error) // ErrLLMSessionNotFound on miss
	// ListSessions returns the tenant's sessions, most-recent first (by started_at DESC).
	// limit <= 0 means no limit.
	ListSessions(ctx context.Context, tenantID string, limit int) ([]*LLMChatSession, error)
	// EndSession sets ended_at (now, RFC3339 UTC) and summary on an existing session.
	EndSession(ctx context.Context, tenantID, chatID, summary string) error // ErrLLMSessionNotFound if absent
	// AppendMessage assigns the next monotonic seq for (tenant, chat) and inserts.
	// It sets m.Seq to the assigned value. The session must already exist.
	AppendMessage(ctx context.Context, m *LLMChatMessage) error
	// ListMessages returns a chat's messages ordered by seq ASC.
	ListMessages(ctx context.Context, tenantID, chatID string) ([]*LLMChatMessage, error)
}
