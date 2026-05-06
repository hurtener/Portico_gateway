package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fixedNow returns a clock function pinned to t.
func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// newTestExchanger builds an Exchanger pointed at srv with deterministic
// clock and minimal config. Callers may override fields on the returned
// pointer before calling Exchange.
func newTestExchanger(t *testing.T, srv *httptest.Server, cfg ExchangeConfig) *Exchanger {
	t.Helper()
	if cfg.TokenURL == "" {
		cfg.TokenURL = srv.URL
	}
	if cfg.ClientID == "" {
		cfg.ClientID = "portico-test"
	}
	ex, err := New(cfg, srv.Client())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ex.WithClock(fixedNow(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	return ex
}

func writeTokenJSON(w http.ResponseWriter, accessToken string, expiresIn int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	body := map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"scope":        "downstream.read",
	}
	if expiresIn > 0 {
		body["expires_in"] = expiresIn
	}
	_ = json.NewEncoder(w).Encode(body)
}

func TestExchange_Success(t *testing.T) {
	t.Parallel()

	var gotForm url.Values
	var gotAuthHeader string
	var gotContentType string
	var gotAccept string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		gotForm = r.PostForm
		gotAuthHeader = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotAccept = r.Header.Get("Accept")
		writeTokenJSON(w, "downstream-access-token", 600)
	}))
	defer srv.Close()

	ex := newTestExchanger(t, srv, ExchangeConfig{
		ClientID:     "portico-test",
		ClientSecret: "shh",
		Audience:     "https://api.example",
		Scope:        "downstream.read downstream.write",
	})

	tok, err := ex.Exchange(context.Background(), "tenant-a", "user-1", "subject-jwt")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if tok.AccessToken != "downstream-access-token" {
		t.Errorf("access_token mismatch: %q", tok.AccessToken)
	}
	if tok.TokenType != "Bearer" {
		t.Errorf("token_type mismatch: %q", tok.TokenType)
	}
	if tok.Scope != "downstream.read" {
		t.Errorf("scope mismatch: %q", tok.Scope)
	}

	wantClock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	wantExpiry := wantClock.Add(600 * time.Second).Add(-30 * time.Second)
	if !tok.ExpiresAt.Equal(wantExpiry) {
		t.Errorf("expires_at = %s, want %s", tok.ExpiresAt, wantExpiry)
	}

	if got := gotForm.Get("grant_type"); got != GrantTypeTokenExchange {
		t.Errorf("grant_type = %q", got)
	}
	if got := gotForm.Get("subject_token"); got != "subject-jwt" {
		t.Errorf("subject_token = %q", got)
	}
	if got := gotForm.Get("subject_token_type"); got != SubjectTokenTypeJWT {
		t.Errorf("subject_token_type = %q", got)
	}
	if got := gotForm.Get("requested_token_type"); got != RequestedTokenTypeAccessToken {
		t.Errorf("requested_token_type = %q", got)
	}
	if got := gotForm.Get("audience"); got != "https://api.example" {
		t.Errorf("audience = %q", got)
	}
	if got := gotForm.Get("scope"); got != "downstream.read downstream.write" {
		t.Errorf("scope = %q", got)
	}
	if !strings.HasPrefix(gotAuthHeader, "Basic ") {
		t.Errorf("auth header = %q, want Basic prefix", gotAuthHeader)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Errorf("content-type = %q", gotContentType)
	}
	if gotAccept != "application/json" {
		t.Errorf("accept = %q", gotAccept)
	}
}

func TestExchange_Cached(t *testing.T) {
	t.Parallel()

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		writeTokenJSON(w, "tok", 600)
	}))
	defer srv.Close()

	ex := newTestExchanger(t, srv, ExchangeConfig{ClientID: "portico-test", Audience: "aud"})

	for i := 0; i < 3; i++ {
		if _, err := ex.Exchange(context.Background(), "t", "u", "subj"); err != nil {
			t.Fatalf("Exchange[%d]: %v", i, err)
		}
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Errorf("idp hits = %d, want 1", got)
	}
}

func TestExchange_CacheKeyHonorsTenantUserAudience(t *testing.T) {
	t.Parallel()

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		writeTokenJSON(w, "tok", 600)
	}))
	defer srv.Close()

	ex := newTestExchanger(t, srv, ExchangeConfig{ClientID: "portico-test", Audience: "aud"})

	calls := []struct{ tenant, user string }{
		{"tenant-a", "user-1"},
		{"tenant-a", "user-2"}, // distinct user
		{"tenant-b", "user-1"}, // distinct tenant
	}
	for _, c := range calls {
		if _, err := ex.Exchange(context.Background(), c.tenant, c.user, "subj"); err != nil {
			t.Fatalf("Exchange(%s,%s): %v", c.tenant, c.user, err)
		}
	}
	if got := atomic.LoadInt64(&hits); got != 3 {
		t.Errorf("idp hits = %d, want 3", got)
	}

	// Repeat one and confirm it now hits cache (still 3).
	if _, err := ex.Exchange(context.Background(), "tenant-a", "user-1", "subj"); err != nil {
		t.Fatalf("Exchange repeat: %v", err)
	}
	if got := atomic.LoadInt64(&hits); got != 3 {
		t.Errorf("idp hits after repeat = %d, want 3", got)
	}
}

func TestExchange_4xxNotRetried(t *testing.T) {
	t.Parallel()

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		// Empty body — confirms the parser handles a 400 with no JSON.
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	ex := newTestExchanger(t, srv, ExchangeConfig{ClientID: "portico-test"})

	_, err := ex.Exchange(context.Background(), "t", "u", "subj")
	if err == nil {
		t.Fatal("expected error")
	}
	var xe *ExchangeError
	if !errors.As(err, &xe) {
		t.Fatalf("error type = %T, want *ExchangeError", err)
	}
	if xe.Status != http.StatusBadRequest {
		t.Errorf("status = %d", xe.Status)
	}
	if xe.Retryable() {
		t.Error("Retryable() = true, want false for 400")
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Errorf("idp hits = %d, want 1 (no retry on 4xx)", got)
	}
}

func TestExchange_5xxRetriedOnce(t *testing.T) {
	t.Parallel()

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&hits, 1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeTokenJSON(w, "tok-after-retry", 600)
	}))
	defer srv.Close()

	ex := newTestExchanger(t, srv, ExchangeConfig{ClientID: "portico-test"})

	tok, err := ex.Exchange(context.Background(), "t", "u", "subj")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if tok.AccessToken != "tok-after-retry" {
		t.Errorf("access_token = %q", tok.AccessToken)
	}
	if got := atomic.LoadInt64(&hits); got != 2 {
		t.Errorf("idp hits = %d, want 2", got)
	}
}

func TestExchange_5xxThenStill5xx(t *testing.T) {
	t.Parallel()

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"temporarily_unavailable"}`))
	}))
	defer srv.Close()

	ex := newTestExchanger(t, srv, ExchangeConfig{ClientID: "portico-test"})

	_, err := ex.Exchange(context.Background(), "t", "u", "subj")
	if err == nil {
		t.Fatal("expected error after second 5xx")
	}
	var xe *ExchangeError
	if !errors.As(err, &xe) {
		t.Fatalf("error type = %T, want *ExchangeError", err)
	}
	if xe.Status != http.StatusServiceUnavailable {
		t.Errorf("status = %d", xe.Status)
	}
	if !xe.Retryable() {
		t.Error("Retryable() = false, want true for 503")
	}
	if got := atomic.LoadInt64(&hits); got != 2 {
		t.Errorf("idp hits = %d, want 2 (one retry)", got)
	}
}

func TestExchange_ExpiresInDefault(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// expires_in omitted entirely.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ex := newTestExchanger(t, srv, ExchangeConfig{ClientID: "portico-test"})
	ex.WithClock(fixedNow(now))

	tok, err := ex.Exchange(context.Background(), "t", "u", "subj")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	want := now.Add(5 * time.Minute).Add(-30 * time.Second)
	if !tok.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %s, want %s", tok.ExpiresAt, want)
	}
}

func TestExchange_NoSubjectToken(t *testing.T) {
	t.Parallel()

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		writeTokenJSON(w, "should-not-happen", 60)
	}))
	defer srv.Close()

	ex := newTestExchanger(t, srv, ExchangeConfig{ClientID: "portico-test"})

	_, err := ex.Exchange(context.Background(), "t", "u", "")
	if !errors.Is(err, ErrNoSubjectToken) {
		t.Fatalf("err = %v, want ErrNoSubjectToken", err)
	}
	if got := atomic.LoadInt64(&hits); got != 0 {
		t.Errorf("idp hits = %d, want 0 (must short-circuit)", got)
	}
}

func TestExchange_BasicAuthHeader(t *testing.T) {
	t.Parallel()

	t.Run("with secret -> basic auth, no client_id in body", func(t *testing.T) {
		t.Parallel()

		var auth string
		var formClientID string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth = r.Header.Get("Authorization")
			_ = r.ParseForm()
			formClientID = r.PostForm.Get("client_id")
			writeTokenJSON(w, "tok", 60)
		}))
		defer srv.Close()

		ex := newTestExchanger(t, srv, ExchangeConfig{ClientID: "portico-test", ClientSecret: "shh"})
		if _, err := ex.Exchange(context.Background(), "t", "u", "subj"); err != nil {
			t.Fatalf("Exchange: %v", err)
		}
		if !strings.HasPrefix(auth, "Basic ") {
			t.Errorf("auth = %q, want Basic prefix", auth)
		}
		if formClientID != "" {
			t.Errorf("client_id in body = %q, want empty when using basic auth", formClientID)
		}
	})

	t.Run("no secret -> client_id in body, no auth header", func(t *testing.T) {
		t.Parallel()

		var auth string
		var formClientID string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth = r.Header.Get("Authorization")
			_ = r.ParseForm()
			formClientID = r.PostForm.Get("client_id")
			writeTokenJSON(w, "tok", 60)
		}))
		defer srv.Close()

		ex := newTestExchanger(t, srv, ExchangeConfig{ClientID: "public-client"})
		if _, err := ex.Exchange(context.Background(), "t", "u", "subj"); err != nil {
			t.Fatalf("Exchange: %v", err)
		}
		if auth != "" {
			t.Errorf("auth = %q, want empty for public client", auth)
		}
		if formClientID != "public-client" {
			t.Errorf("client_id in body = %q, want public-client", formClientID)
		}
	})
}

func TestExchange_ContextCancelled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If we reach here the test failed: context should have been
		// cancelled before we got the request.
		writeTokenJSON(w, "should-not-happen", 60)
	}))
	defer srv.Close()

	ex := newTestExchanger(t, srv, ExchangeConfig{ClientID: "portico-test"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ex.Exchange(ctx, "t", "u", "subj")
	if err == nil {
		t.Fatal("expected error from cancelled ctx")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want wraps context.Canceled", err)
	}
}

func TestExchangeError_AuditPayload_NoTokens(t *testing.T) {
	t.Parallel()

	hostile := strings.Repeat("subject_token=eyJhbGciOi.PAYLOAD.SIG ", 50) // > 200 chars
	xe := &ExchangeError{
		Status: 400,
		Code:   "invalid_grant",
		Desc:   hostile,
	}

	payload := xe.AuditPayload()
	if payload["status"] != 400 {
		t.Errorf("status = %v", payload["status"])
	}
	if payload["error_code"] != "invalid_grant" {
		t.Errorf("error_code = %v", payload["error_code"])
	}

	desc, _ := payload["error_description"].(string)
	// Must be truncated. The implementation slices to 200 runes/bytes
	// and appends an ellipsis, so length should be 200 + len("…").
	if len(desc) > 220 {
		t.Errorf("error_description length = %d, want <= 220 (truncated)", len(desc))
	}
	if len(desc) >= len(hostile) {
		t.Errorf("error_description not truncated: %d >= %d", len(desc), len(hostile))
	}

	// Sanity: the payload must serialize to JSON cleanly (no token fields).
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, forbidden := range []string{"access_token", "subject_token_type", "refresh_token"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("payload contains forbidden key %q: %s", forbidden, raw)
		}
	}

	// Nil safety.
	var nilXE *ExchangeError
	if got := nilXE.AuditPayload(); got != nil {
		t.Errorf("nil AuditPayload = %v, want nil", got)
	}
}

// Compile-time assertion that *ExchangeError implements error.
var _ error = (*ExchangeError)(nil)
