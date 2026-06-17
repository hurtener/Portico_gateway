package sqlite_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

func TestLLMSessionStore_CreateGetRoundTrip(t *testing.T) {
	db := open(t)
	store := db.LLMSessions()
	ctx := context.Background()

	sess := &ifaces.LLMChatSession{
		TenantID:  "tenant-a",
		ChatID:    "01HE0000000000000000000001",
		UserID:    "user-123",
		Alias:     "gpt-4",
		StartedAt: "2025-01-15T10:00:00Z",
	}
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	got, err := store.GetSession(ctx, "tenant-a", "01HE0000000000000000000001")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.TenantID != "tenant-a" || got.ChatID != "01HE0000000000000000000001" || got.UserID != "user-123" || got.Alias != "gpt-4" || got.StartedAt != "2025-01-15T10:00:00Z" || got.EndedAt != "" || got.Summary != "" {
		t.Errorf("get mismatch: %+v", got)
	}

	// Get non-existent returns ErrLLMSessionNotFound
	_, err = store.GetSession(ctx, "tenant-a", "nonexistent")
	if !errors.Is(err, ifaces.ErrLLMSessionNotFound) {
		t.Fatalf("expected ErrLLMSessionNotFound, got: %v", err)
	}
}

func TestLLMSessionStore_ListSessions_MostRecentFirst_TenantFiltered(t *testing.T) {
	db := open(t)
	store := db.LLMSessions()
	ctx := context.Background()

	// Add sessions for tenant-a
	if err := store.CreateSession(ctx, &ifaces.LLMChatSession{
		TenantID: "tenant-a", ChatID: "chat-1", Alias: "gpt-4", StartedAt: "2025-01-15T10:00:00Z",
	}); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if err := store.CreateSession(ctx, &ifaces.LLMChatSession{
		TenantID: "tenant-a", ChatID: "chat-2", Alias: "gpt-4", StartedAt: "2025-01-15T11:00:00Z",
	}); err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if err := store.CreateSession(ctx, &ifaces.LLMChatSession{
		TenantID: "tenant-a", ChatID: "chat-3", Alias: "claude-3", StartedAt: "2025-01-15T09:00:00Z",
	}); err != nil {
		t.Fatalf("create 3: %v", err)
	}

	// Add session for tenant-b (should not appear in tenant-a's list)
	if err := store.CreateSession(ctx, &ifaces.LLMChatSession{
		TenantID: "tenant-b", ChatID: "chat-1", Alias: "gpt-4", StartedAt: "2025-01-15T12:00:00Z",
	}); err != nil {
		t.Fatalf("create tenant-b: %v", err)
	}

	// List tenant-a - should be most-recent first (chat-2, chat-1, chat-3)
	listA, err := store.ListSessions(ctx, "tenant-a", 0)
	if err != nil {
		t.Fatalf("list tenant-a: %v", err)
	}
	if len(listA) != 3 {
		t.Fatalf("tenant-a sessions = %d, want 3", len(listA))
	}
	if listA[0].ChatID != "chat-2" || listA[1].ChatID != "chat-1" || listA[2].ChatID != "chat-3" {
		t.Errorf("order wrong: got %s, %s, %s", listA[0].ChatID, listA[1].ChatID, listA[2].ChatID)
	}
	for _, s := range listA {
		if s.TenantID != "tenant-a" {
			t.Errorf("cross-tenant leak in list: %+v", s)
		}
	}

	// Limit
	listALimited, err := store.ListSessions(ctx, "tenant-a", 2)
	if err != nil {
		t.Fatalf("list limited: %v", err)
	}
	if len(listALimited) != 2 {
		t.Errorf("limited = %d, want 2", len(listALimited))
	}
	if listALimited[0].ChatID != "chat-2" || listALimited[1].ChatID != "chat-1" {
		t.Errorf("limited order wrong: %s, %s", listALimited[0].ChatID, listALimited[1].ChatID)
	}

	// List tenant-b should only have 1
	listB, err := store.ListSessions(ctx, "tenant-b", 0)
	if err != nil {
		t.Fatalf("list tenant-b: %v", err)
	}
	if len(listB) != 1 || listB[0].ChatID != "chat-1" {
		t.Errorf("tenant-b sessions: %+v", listB)
	}
}

func TestLLMSessionStore_EndSession(t *testing.T) {
	db := open(t)
	store := db.LLMSessions()
	ctx := context.Background()

	// Create session
	if err := store.CreateSession(ctx, &ifaces.LLMChatSession{
		TenantID: "tenant-a", ChatID: "chat-end", Alias: "gpt-4", StartedAt: "2025-01-15T10:00:00Z",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// End session
	if err := store.EndSession(ctx, "tenant-a", "chat-end", "Test summary"); err != nil {
		t.Fatalf("end session: %v", err)
	}

	// Verify ended_at and summary set
	got, err := store.GetSession(ctx, "tenant-a", "chat-end")
	if err != nil {
		t.Fatalf("get after end: %v", err)
	}
	if got.EndedAt == "" {
		t.Errorf("ended_at not set: %+v", got)
	}
	if got.Summary != "Test summary" {
		t.Errorf("summary not set: got %q", got.Summary)
	}

	// End non-existent returns ErrLLMSessionNotFound
	err = store.EndSession(ctx, "tenant-a", "nonexistent", "summary")
	if !errors.Is(err, ifaces.ErrLLMSessionNotFound) {
		t.Fatalf("expected ErrLLMSessionNotFound, got: %v", err)
	}
}

func TestLLMSessionStore_AppendMessage_SeqMonotonic(t *testing.T) {
	db := open(t)
	store := db.LLMSessions()
	ctx := context.Background()

	// Create session first
	if err := store.CreateSession(ctx, &ifaces.LLMChatSession{
		TenantID: "tenant-a", ChatID: "chat-msg", Alias: "gpt-4", StartedAt: "2025-01-15T10:00:00Z",
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Append messages
	msg1 := &ifaces.LLMChatMessage{
		TenantID:    "tenant-a",
		ChatID:      "chat-msg",
		Role:        "user",
		ContentJSON: `{"content": "Hello"}`,
		SpanID:      "span-1",
	}
	if err := store.AppendMessage(ctx, msg1); err != nil {
		t.Fatalf("append msg1: %v", err)
	}
	if msg1.Seq != 1 {
		t.Errorf("msg1 seq = %d, want 1", msg1.Seq)
	}
	if msg1.CreatedAt == "" {
		t.Errorf("msg1 created_at not set")
	}

	msg2 := &ifaces.LLMChatMessage{
		TenantID:    "tenant-a",
		ChatID:      "chat-msg",
		Role:        "assistant",
		ContentJSON: `{"content": "Hi there"}`,
		ToolCallID:  "call-123",
		SpanID:      "span-2",
	}
	if err := store.AppendMessage(ctx, msg2); err != nil {
		t.Fatalf("append msg2: %v", err)
	}
	if msg2.Seq != 2 {
		t.Errorf("msg2 seq = %d, want 2", msg2.Seq)
	}

	msg3 := &ifaces.LLMChatMessage{
		TenantID:    "tenant-a",
		ChatID:      "chat-msg",
		Role:        "tool",
		ContentJSON: `{"result": "ok"}`,
		ToolCallID:  "call-123",
		SpanID:      "span-3",
	}
	if err := store.AppendMessage(ctx, msg3); err != nil {
		t.Fatalf("append msg3: %v", err)
	}
	if msg3.Seq != 3 {
		t.Errorf("msg3 seq = %d, want 3", msg3.Seq)
	}

	// List messages - should be ordered by seq ASC
	msgs, err := store.ListMessages(ctx, "tenant-a", "chat-msg")
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	if msgs[0].Seq != 1 || msgs[0].Role != "user" {
		t.Errorf("msg 0: %+v", msgs[0])
	}
	if msgs[1].Seq != 2 || msgs[1].Role != "assistant" || msgs[1].ToolCallID != "call-123" {
		t.Errorf("msg 1: %+v", msgs[1])
	}
	if msgs[2].Seq != 3 || msgs[2].Role != "tool" || msgs[2].ToolCallID != "call-123" {
		t.Errorf("msg 2: %+v", msgs[2])
	}
}

func TestLLMSessionStore_CrossTenantIsolation(t *testing.T) {
	db := open(t)
	store := db.LLMSessions()
	ctx := context.Background()

	// Tenant A creates session and messages
	if err := store.CreateSession(ctx, &ifaces.LLMChatSession{
		TenantID: "tenant-a", ChatID: "chat-a", Alias: "gpt-4", StartedAt: "2025-01-15T10:00:00Z",
	}); err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	if err := store.AppendMessage(ctx, &ifaces.LLMChatMessage{
		TenantID: "tenant-a", ChatID: "chat-a", Role: "user", ContentJSON: `{"c": "A"}`, SpanID: "s1",
	}); err != nil {
		t.Fatalf("append A: %v", err)
	}

	// Tenant B creates their own session
	if err := store.CreateSession(ctx, &ifaces.LLMChatSession{
		TenantID: "tenant-b", ChatID: "chat-b", Alias: "claude-3", StartedAt: "2025-01-15T11:00:00Z",
	}); err != nil {
		t.Fatalf("create tenant B: %v", err)
	}
	if err := store.AppendMessage(ctx, &ifaces.LLMChatMessage{
		TenantID: "tenant-b", ChatID: "chat-b", Role: "user", ContentJSON: `{"c": "B"}`, SpanID: "s2",
	}); err != nil {
		t.Fatalf("append B: %v", err)
	}

	// Tenant A cannot GetSession tenant B's chat
	_, err := store.GetSession(ctx, "tenant-a", "chat-b")
	if !errors.Is(err, ifaces.ErrLLMSessionNotFound) {
		t.Fatalf("tenant-a get tenant-b session: expected ErrLLMSessionNotFound, got: %v", err)
	}

	// Tenant B cannot GetSession tenant A's chat
	_, err = store.GetSession(ctx, "tenant-b", "chat-a")
	if !errors.Is(err, ifaces.ErrLLMSessionNotFound) {
		t.Fatalf("tenant-b get tenant-a session: expected ErrLLMSessionNotFound, got: %v", err)
	}

	// Tenant A cannot ListMessages tenant B's chat
	msgsB, err := store.ListMessages(ctx, "tenant-a", "chat-b")
	if err != nil {
		t.Fatalf("tenant-a list tenant-b messages: %v", err)
	}
	if len(msgsB) != 0 {
		t.Errorf("tenant-a should see 0 messages for tenant-b's chat, got %d", len(msgsB))
	}

	// Tenant B cannot ListMessages tenant A's chat
	msgsA, err := store.ListMessages(ctx, "tenant-b", "chat-a")
	if err != nil {
		t.Fatalf("tenant-b list tenant-a messages: %v", err)
	}
	if len(msgsA) != 0 {
		t.Errorf("tenant-b should see 0 messages for tenant-a's chat, got %d", len(msgsA))
	}

	// ListSessions for A excludes B's rows
	listA, err := store.ListSessions(ctx, "tenant-a", 0)
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(listA) != 1 || listA[0].ChatID != "chat-a" {
		t.Errorf("tenant-a list: %+v", listA)
	}

	// ListSessions for B excludes A's rows
	listB, err := store.ListSessions(ctx, "tenant-b", 0)
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(listB) != 1 || listB[0].ChatID != "chat-b" {
		t.Errorf("tenant-b list: %+v", listB)
	}
}

func TestLLMSessionStore_CascadeDelete(t *testing.T) {
	db := open(t)
	store := db.LLMSessions()
	ctx := context.Background()

	// Create session with messages
	if err := store.CreateSession(ctx, &ifaces.LLMChatSession{
		TenantID: "tenant-a", ChatID: "chat-cascade", Alias: "gpt-4", StartedAt: "2025-01-15T10:00:00Z",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.AppendMessage(ctx, &ifaces.LLMChatMessage{
		TenantID: "tenant-a", ChatID: "chat-cascade", Role: "user", ContentJSON: `{"c": "1"}`, SpanID: "s1",
	}); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := store.AppendMessage(ctx, &ifaces.LLMChatMessage{
		TenantID: "tenant-a", ChatID: "chat-cascade", Role: "assistant", ContentJSON: `{"c": "2"}`, SpanID: "s2",
	}); err != nil {
		t.Fatalf("append 2: %v", err)
	}

	// Verify messages exist
	msgs, err := store.ListMessages(ctx, "tenant-a", "chat-cascade")
	if err != nil {
		t.Fatalf("list before delete: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages before delete, got %d", len(msgs))
	}

	// Delete session by ending it then attempting to get (FK cascade is on delete of session row)
	// Note: The DDL has ON DELETE CASCADE on the FK, but we don't have a DeleteSession method.
	// We verify the cascade works at the SQL level by directly deleting the session row.
	_, err = db.SQL().ExecContext(ctx, `DELETE FROM tenant_llm_sessions WHERE tenant_id = ? AND chat_id = ?`, "tenant-a", "chat-cascade")
	if err != nil {
		t.Fatalf("direct delete session: %v", err)
	}

	// Messages should be cascade-deleted
	msgs, err = store.ListMessages(ctx, "tenant-a", "chat-cascade")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("messages should be cascade-deleted, got %d", len(msgs))
	}

	// Session should be gone
	_, err = store.GetSession(ctx, "tenant-a", "chat-cascade")
	if !errors.Is(err, ifaces.ErrLLMSessionNotFound) {
		t.Fatalf("expected ErrLLMSessionNotFound after cascade delete, got: %v", err)
	}
}

func TestLLMSessionStore_EmptyUserID_EndedAt_Summary_Nullable(t *testing.T) {
	db := open(t)
	store := db.LLMSessions()
	ctx := context.Background()

	// Create with empty UserID, EndedAt, Summary
	sess := &ifaces.LLMChatSession{
		TenantID:  "tenant-a",
		ChatID:    "chat-nulls",
		UserID:    "",
		Alias:     "gpt-4",
		StartedAt: "2025-01-15T10:00:00Z",
		EndedAt:   "",
		Summary:   "",
	}
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create with empty nullable: %v", err)
	}

	got, err := store.GetSession(ctx, "tenant-a", "chat-nulls")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.UserID != "" || got.EndedAt != "" || got.Summary != "" {
		t.Errorf("nullable fields should be empty: %+v", got)
	}

	// EndSession with empty summary should set ended_at but keep summary empty
	if err := store.EndSession(ctx, "tenant-a", "chat-nulls", ""); err != nil {
		t.Fatalf("end with empty summary: %v", err)
	}
	got, err = store.GetSession(ctx, "tenant-a", "chat-nulls")
	if err != nil {
		t.Fatalf("get after end: %v", err)
	}
	if got.EndedAt == "" {
		t.Errorf("ended_at should be set")
	}
	if got.Summary != "" {
		t.Errorf("summary should remain empty, got %q", got.Summary)
	}
}
