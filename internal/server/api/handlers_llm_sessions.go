package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// defaultSessionListLimit caps an unbounded /api/llm/sessions list.
const defaultSessionListLimit = 100

// ===== DTOs ===============================================================

// LLMSessionListItem is a summary row for /api/llm/sessions.
type LLMSessionListItem struct {
	ChatID    string `json:"chat_id"`
	Alias     string `json:"alias"`
	UserID    string `json:"user_id,omitempty"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

// LLMSessionTranscript is the full session with messages for
// GET /api/llm/sessions/{chat_id}.
type LLMSessionTranscript struct {
	ChatID    string              `json:"chat_id"`
	Alias     string              `json:"alias"`
	UserID    string              `json:"user_id,omitempty"`
	StartedAt string              `json:"started_at"`
	EndedAt   string              `json:"ended_at,omitempty"`
	Summary   string              `json:"summary,omitempty"`
	Messages  []LLMSessionMessage `json:"messages"`
}

// LLMSessionMessage is a single message in the transcript.
type LLMSessionMessage struct {
	Seq         int    `json:"seq"`
	Role        string `json:"role"`
	ContentJSON string `json:"content"`
	ToolCallID  string `json:"tool_call_id,omitempty"`
	SpanID      string `json:"span_id,omitempty"`
	CreatedAt   string `json:"timestamp"`
}

// ===== Handlers ===========================================================

// listLLMSessionsHandler handles GET /api/llm/sessions.
// Requires llm:admin scope (or admin scope).
func listLLMSessionsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMSessions == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm session store not configured", nil)
			return
		}
		id := tenant.MustFrom(r.Context())
		if !requireLLMAdmin(w, id) {
			return
		}

		limit := defaultSessionListLimit
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}

		ctx := r.Context()
		sessions, err := d.LLMSessions.ListSessions(ctx, id.TenantID, limit)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_failed", err.Error(), nil)
			return
		}

		out := make([]LLMSessionListItem, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, LLMSessionListItem{
				ChatID:    s.ChatID,
				Alias:     s.Alias,
				UserID:    s.UserID,
				StartedAt: s.StartedAt,
				EndedAt:   s.EndedAt,
				Summary:   s.Summary,
			})
		}

		writeJSON(w, http.StatusOK, out)
	}
}

// getLLMSessionHandler handles GET /api/llm/sessions/{chat_id}.
// Returns the full transcript with messages (redacted per policy).
func getLLMSessionHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.LLMSessions == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "llm_not_configured", "llm session store not configured", nil)
			return
		}

		id := tenant.MustFrom(r.Context())
		if !requireLLMAdmin(w, id) {
			return
		}

		ctx := r.Context()
		chatID := chi.URLParam(r, "chat_id")

		// Get session metadata
		sess, err := d.LLMSessions.GetSession(ctx, id.TenantID, chatID)
		if err != nil {
			if errors.Is(err, ifaces.ErrLLMSessionNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "session not found", nil)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "get_failed", err.Error(), nil)
			return
		}

		// Get messages
		msgs, err := d.LLMSessions.ListMessages(ctx, id.TenantID, chatID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "list_messages_failed", err.Error(), nil)
			return
		}

		out := LLMSessionTranscript{
			ChatID:    sess.ChatID,
			Alias:     sess.Alias,
			UserID:    sess.UserID,
			StartedAt: sess.StartedAt,
			EndedAt:   sess.EndedAt,
			Summary:   sess.Summary,
			Messages:  make([]LLMSessionMessage, 0, len(msgs)),
		}

		for _, m := range msgs {
			out.Messages = append(out.Messages, LLMSessionMessage{
				Seq:         m.Seq,
				Role:        m.Role,
				ContentJSON: m.ContentJSON,
				ToolCallID:  m.ToolCallID,
				SpanID:      m.SpanID,
				CreatedAt:   m.CreatedAt,
			})
		}

		writeJSON(w, http.StatusOK, out)
	}
}
