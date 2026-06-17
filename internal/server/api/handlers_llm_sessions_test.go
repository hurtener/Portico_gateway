package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// ---- LLMSessionStore stub ------------------------------------------------

type stubLLMSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*ifaces.LLMChatSession   // tenant/chatID -> session
	msgs     map[string][]*ifaces.LLMChatMessage // tenant/chatID -> messages
	failList bool
	failGet  bool
	failMsg  bool
}

func newStubLLMSessionStore() *stubLLMSessionStore {
	return &stubLLMSessionStore{
		sessions: map[string]*ifaces.LLMChatSession{},
		msgs:     map[string][]*ifaces.LLMChatMessage{},
	}
}

func sessionKey(tenantID, chatID string) string { return tenantID + "/" + chatID }

func (s *stubLLMSessionStore) seed(t *testing.T, tid, chatID, alias string, msgs []*ifaces.LLMChatMessage) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	s.sessions[sessionKey(tid, chatID)] = &ifaces.LLMChatSession{
		ChatID:    chatID,
		TenantID:  tid,
		Alias:     alias,
		UserID:    "tester",
		StartedAt: now,
		EndedAt:   "",
		Summary:   "",
	}
	s.msgs[sessionKey(tid, chatID)] = msgs
}

func (s *stubLLMSessionStore) ListSessions(ctx context.Context, tenantID string, limit int) ([]*ifaces.LLMChatSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failList {
		return nil, errors.New("list sessions boom")
	}
	out := []*ifaces.LLMChatSession{}
	for _, sess := range s.sessions {
		if sess.TenantID == tenantID {
			cp := *sess
			out = append(out, &cp)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (s *stubLLMSessionStore) GetSession(ctx context.Context, tenantID, chatID string) (*ifaces.LLMChatSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failGet {
		return nil, errors.New("get session boom")
	}
	key := sessionKey(tenantID, chatID)
	sess, ok := s.sessions[key]
	if !ok {
		return nil, ifaces.ErrLLMSessionNotFound
	}
	cp := *sess
	return &cp, nil
}

func (s *stubLLMSessionStore) ListMessages(ctx context.Context, tenantID, chatID string) ([]*ifaces.LLMChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failMsg {
		return nil, errors.New("list messages boom")
	}
	key := sessionKey(tenantID, chatID)
	msgs, ok := s.msgs[key]
	if !ok {
		return nil, ifaces.ErrLLMSessionNotFound
	}
	out := make([]*ifaces.LLMChatMessage, len(msgs))
	for i, m := range msgs {
		cp := *m
		out[i] = &cp
	}
	return out, nil
}

func (s *stubLLMSessionStore) CreateSession(ctx context.Context, sess *ifaces.LLMChatSession) error {
	return nil
}
func (s *stubLLMSessionStore) EndSession(ctx context.Context, tenantID, chatID, summary string) error {
	return nil
}
func (s *stubLLMSessionStore) AppendMessage(ctx context.Context, msg *ifaces.LLMChatMessage) error {
	return nil
}

// ---- Tests ---------------------------------------------------------------

func TestListLLMSessions_Success(t *testing.T) {
	d, _ := llmDeps()
	store := newStubLLMSessionStore()
	d.LLMSessions = store

	// seed two sessions for tenant t1
	now := time.Now().UTC().Format(time.RFC3339)
	store.seed(t, "t1", "chat-1", "gpt-4", []*ifaces.LLMChatMessage{
		{Role: "user", ContentJSON: "hello", Seq: 1, CreatedAt: now},
		{Role: "assistant", ContentJSON: "hi there", Seq: 2, CreatedAt: now},
	})
	store.seed(t, "t1", "chat-2", "gpt-3.5-turbo", []*ifaces.LLMChatMessage{
		{Role: "user", ContentJSON: "test", Seq: 1, CreatedAt: now},
	})

	r := newReq(http.MethodGet, "/api/llm/sessions", nil, "admin")
	w := runHandler(listLLMSessionsHandler(d), r)

	statusOK(t, w, http.StatusOK)

	var out []LLMSessionListItem
	decodeJSON(t, w, &out)
	if len(out) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(out))
	}
	if out[0].ChatID != "chat-1" || out[0].Alias != "gpt-4" {
		t.Fatalf("unexpected first session: %+v", out[0])
	}
	if out[1].ChatID != "chat-2" || out[1].Alias != "gpt-3.5-turbo" {
		t.Fatalf("unexpected second session: %+v", out[1])
	}
}

func TestListLLMSessions_StoreNil_Returns503(t *testing.T) {
	d, _ := llmDeps()
	d.LLMSessions = nil // nil store

	r := newReq(http.MethodGet, "/api/llm/sessions", nil, "admin")
	w := runHandler(listLLMSessionsHandler(d), r)

	statusOK(t, w, http.StatusServiceUnavailable)
}

func TestListLLMSessions_StoreError_Returns500(t *testing.T) {
	d, _ := llmDeps()
	store := newStubLLMSessionStore()
	store.failList = true
	d.LLMSessions = store

	r := newReq(http.MethodGet, "/api/llm/sessions", nil, "admin")
	w := runHandler(listLLMSessionsHandler(d), r)

	statusOK(t, w, http.StatusInternalServerError)
}

func TestListLLMSessions_TenantIsolation(t *testing.T) {
	d, _ := llmDeps()
	store := newStubLLMSessionStore()
	d.LLMSessions = store

	now := time.Now().UTC().Format(time.RFC3339)
	store.seed(t, "t1", "chat-t1", "gpt-4", []*ifaces.LLMChatMessage{
		{Role: "user", ContentJSON: "t1 secret", Seq: 1, CreatedAt: now},
	})
	store.seed(t, "t2", "chat-t2", "gpt-4", []*ifaces.LLMChatMessage{
		{Role: "user", ContentJSON: "t2 secret", Seq: 1, CreatedAt: now},
	})

	// request as t1
	r := newReq(http.MethodGet, "/api/llm/sessions", nil, "admin")
	r = r.WithContext(tenant.With(r.Context(), tenant.Identity{TenantID: "t1", UserID: "tester", Scopes: []string{"admin"}}))
	w := runHandler(listLLMSessionsHandler(d), r)

	statusOK(t, w, http.StatusOK)

	var out []LLMSessionListItem
	decodeJSON(t, w, &out)
	if len(out) != 1 {
		t.Fatalf("want 1 session for t1, got %d", len(out))
	}
	if out[0].ChatID != "chat-t1" {
		t.Fatalf("want chat-t1, got %s", out[0].ChatID)
	}
}

func TestGetLLMSession_Success(t *testing.T) {
	d, _ := llmDeps()
	store := newStubLLMSessionStore()
	d.LLMSessions = store

	now := time.Now().UTC().Format(time.RFC3339)
	msgs := []*ifaces.LLMChatMessage{
		{Role: "user", ContentJSON: "hello", Seq: 1, CreatedAt: now},
		{Role: "assistant", ContentJSON: "hi there", Seq: 2, CreatedAt: now},
	}
	store.seed(t, "t1", "chat-1", "gpt-4", msgs)

	r := newReq(http.MethodGet, "/api/llm/sessions/chat-1", nil, "admin")
	r = withChiURLParam(r, "chat_id", "chat-1")
	w := runHandler(getLLMSessionHandler(d), r)

	statusOK(t, w, http.StatusOK)

	var out LLMSessionTranscript
	decodeJSON(t, w, &out)

	if out.ChatID != "chat-1" {
		t.Fatalf("want chat-1, got %s", out.ChatID)
	}
	if out.Alias != "gpt-4" {
		t.Fatalf("want gpt-4, got %s", out.Alias)
	}
	if len(out.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d", len(out.Messages))
	}
	if out.Messages[0].Role != "user" || out.Messages[0].ContentJSON != "hello" {
		t.Fatalf("unexpected message 0: %+v", out.Messages[0])
	}
	if out.Messages[1].Role != "assistant" || out.Messages[1].ContentJSON != "hi there" {
		t.Fatalf("unexpected message 1: %+v", out.Messages[1])
	}
}

func TestGetLLMSession_NotFound_Returns404(t *testing.T) {
	d, _ := llmDeps()
	store := newStubLLMSessionStore()
	d.LLMSessions = store

	r := newReq(http.MethodGet, "/api/llm/sessions/unknown", nil, "admin")
	r = withChiURLParam(r, "chat_id", "unknown")
	w := runHandler(getLLMSessionHandler(d), r)

	statusOK(t, w, http.StatusNotFound)

	var body map[string]any
	decodeJSON(t, w, &body)
	if body["message"] != "session not found" {
		t.Fatalf("unexpected error body: %+v", body)
	}
}

func TestGetLLMSession_StoreNil_Returns503(t *testing.T) {
	d, _ := llmDeps()
	d.LLMSessions = nil

	r := newReq(http.MethodGet, "/api/llm/sessions/chat-1", nil, "admin")
	r = withChiURLParam(r, "chat_id", "chat-1")
	w := runHandler(getLLMSessionHandler(d), r)

	statusOK(t, w, http.StatusServiceUnavailable)
}

func TestGetLLMSession_GetSessionError_Returns500(t *testing.T) {
	d, _ := llmDeps()
	store := newStubLLMSessionStore()
	store.failGet = true
	d.LLMSessions = store

	r := newReq(http.MethodGet, "/api/llm/sessions/chat-1", nil, "admin")
	r = withChiURLParam(r, "chat_id", "chat-1")
	w := runHandler(getLLMSessionHandler(d), r)

	statusOK(t, w, http.StatusInternalServerError)
}

func TestGetLLMSession_ListMessagesError_Returns500(t *testing.T) {
	d, _ := llmDeps()
	store := newStubLLMSessionStore()
	store.failMsg = true
	d.LLMSessions = store
	store.seed(t, "t1", "chat-1", "gpt-4", []*ifaces.LLMChatMessage{})

	r := newReq(http.MethodGet, "/api/llm/sessions/chat-1", nil, "admin")
	r = withChiURLParam(r, "chat_id", "chat-1")
	w := runHandler(getLLMSessionHandler(d), r)

	statusOK(t, w, http.StatusInternalServerError)
}

func TestGetLLMSession_TenantIsolation(t *testing.T) {
	d, _ := llmDeps()
	store := newStubLLMSessionStore()
	d.LLMSessions = store

	now := time.Now().UTC().Format(time.RFC3339)
	store.seed(t, "t1", "chat-1", "gpt-4", []*ifaces.LLMChatMessage{
		{Role: "user", ContentJSON: "t1 secret", Seq: 1, CreatedAt: now},
	})
	store.seed(t, "t2", "chat-1", "gpt-4", []*ifaces.LLMChatMessage{
		{Role: "user", ContentJSON: "t2 secret", Seq: 1, CreatedAt: now},
	})

	// request as t1 for chat-1
	r := newReq(http.MethodGet, "/api/llm/sessions/chat-1", nil, "admin")
	r = r.WithContext(tenant.With(r.Context(), tenant.Identity{TenantID: "t1", UserID: "tester", Scopes: []string{"admin"}}))
	r = withChiURLParam(r, "chat_id", "chat-1")
	w := runHandler(getLLMSessionHandler(d), r)

	statusOK(t, w, http.StatusOK)

	var out LLMSessionTranscript
	decodeJSON(t, w, &out)
	if out.Messages[0].ContentJSON != "t1 secret" {
		t.Fatalf("tenant isolation failed: got %s", out.Messages[0].ContentJSON)
	}
}
