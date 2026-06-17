package api

import (
	"net/http"

	"github.com/oklog/ulid/v2"

	"github.com/hurtener/Portico_gateway/internal/audit"
	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// sessionSummaryMaxRunes bounds the summary derived from the first user message.
const sessionSummaryMaxRunes = 120

// recordChatSession persists a brokered chat completion as a session plus its
// messages. Message content is run through the audit redactor BEFORE write so
// secrets that appear in prompts/responses never land in storage (§7). It is
// best-effort: any failure is swallowed so session telemetry can never break a
// served request, and it is a no-op when the session store is not wired.
func recordChatSession(d Deps, r *http.Request, alias string, reqMsgs []openAIMessage, respMsg openAIMessage) {
	if d.LLMSessions == nil {
		return
	}
	id := tenant.MustFrom(r.Context())
	ctx := r.Context()

	red := d.Redactor
	if red == nil {
		red = audit.NewDefaultRedactor()
	}

	chatID := ulid.Make().String()
	if err := d.LLMSessions.CreateSession(ctx, &ifaces.LLMChatSession{
		TenantID: id.TenantID,
		ChatID:   chatID,
		UserID:   id.UserID,
		Alias:    alias,
	}); err != nil {
		return
	}

	appendMsg := func(role, content string) {
		_ = d.LLMSessions.AppendMessage(ctx, &ifaces.LLMChatMessage{
			TenantID:    id.TenantID,
			ChatID:      chatID,
			Role:        role,
			ContentJSON: redactContent(red, content),
		})
	}

	var firstUser string
	for _, m := range reqMsgs {
		appendMsg(m.Role, m.Content)
		if firstUser == "" && m.Role == "user" {
			firstUser = m.Content
		}
	}
	appendMsg(respMsg.Role, respMsg.Content)

	summary := redactContent(red, truncateRunes(firstUser, sessionSummaryMaxRunes))
	_ = d.LLMSessions.EndSession(ctx, id.TenantID, chatID, summary)
}

// redactContent masks known-credential shapes in a free-text message body.
func redactContent(red *audit.Redactor, s string) string {
	if s == "" {
		return s
	}
	out := red.Redact(map[string]any{"v": s})
	if rv, ok := out["v"].(string); ok {
		return rv
	}
	return s
}

// truncateRunes shortens s to at most n runes without splitting a code point.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
