package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// capturingSessionStore records every write so the recording helper's behaviour
// (session + ordered messages + redaction + summary) can be asserted.
type capturingSessionStore struct {
	created  []*ifaces.LLMChatSession
	appended []*ifaces.LLMChatMessage
	ended    []string
}

func (c *capturingSessionStore) CreateSession(_ context.Context, s *ifaces.LLMChatSession) error {
	c.created = append(c.created, s)
	return nil
}

func (c *capturingSessionStore) AppendMessage(_ context.Context, m *ifaces.LLMChatMessage) error {
	c.appended = append(c.appended, m)
	return nil
}

func (c *capturingSessionStore) EndSession(_ context.Context, _, _, summary string) error {
	c.ended = append(c.ended, summary)
	return nil
}

func (c *capturingSessionStore) GetSession(context.Context, string, string) (*ifaces.LLMChatSession, error) {
	return nil, ifaces.ErrLLMSessionNotFound
}

func (c *capturingSessionStore) ListSessions(context.Context, string, int) ([]*ifaces.LLMChatSession, error) {
	return nil, nil
}

func (c *capturingSessionStore) ListMessages(context.Context, string, string) ([]*ifaces.LLMChatMessage, error) {
	return nil, nil
}

func TestRecordChatSession_RecordsAndRedacts(t *testing.T) {
	d, _ := llmDeps()
	store := &capturingSessionStore{}
	d.LLMSessions = store
	// d.Redactor left nil — the helper must fall back to a default redactor.

	secret := "ghp_" + strings.Repeat("a", 36) // matches the github-token pattern
	reqMsgs := []openAIMessage{
		{Role: "system", Content: "be helpful"},
		{Role: "user", Content: "my token is " + secret + " thanks"},
	}
	resp := openAIMessage{Role: "assistant", Content: "noted"}

	r := newReq(http.MethodPost, "/v1/chat/completions", nil, ScopeLLMInvoke)
	recordChatSession(d, r, "gpt-4", reqMsgs, resp)

	if len(store.created) != 1 {
		t.Fatalf("want 1 session created, got %d", len(store.created))
	}
	if store.created[0].ChatID == "" || store.created[0].Alias != "gpt-4" {
		t.Errorf("session metadata wrong: %+v", store.created[0])
	}
	if len(store.appended) != 3 {
		t.Fatalf("want 3 messages (2 request + 1 response), got %d", len(store.appended))
	}
	if store.appended[0].Role != "system" || store.appended[1].Role != "user" || store.appended[2].Role != "assistant" {
		t.Errorf("message order wrong: %v/%v/%v", store.appended[0].Role, store.appended[1].Role, store.appended[2].Role)
	}
	// The secret must NOT survive into stored content, and the redaction marker
	// must be present.
	userContent := store.appended[1].ContentJSON
	if strings.Contains(userContent, secret) {
		t.Errorf("secret leaked into stored content: %q", userContent)
	}
	if !strings.Contains(userContent, "REDACTED") {
		t.Errorf("expected a redaction marker in %q", userContent)
	}
	if len(store.ended) != 1 {
		t.Fatalf("want EndSession called once, got %d", len(store.ended))
	}
	if strings.Contains(store.ended[0], secret) {
		t.Errorf("secret leaked into summary: %q", store.ended[0])
	}
}

func TestRecordChatSession_NilStore_NoOp(t *testing.T) {
	d, _ := llmDeps()
	d.LLMSessions = nil
	r := newReq(http.MethodPost, "/v1/chat/completions", nil, ScopeLLMInvoke)
	// Must not panic and must be a no-op.
	recordChatSession(d, r, "gpt-4",
		[]openAIMessage{{Role: "user", Content: "hi"}},
		openAIMessage{Role: "assistant", Content: "yo"})
}
