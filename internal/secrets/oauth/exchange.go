// Package oauth implements RFC 8693 Token Exchange against the IdP an
// operator configures per server. The Exchanger is keyed (tenant, user,
// audience): two users on the same tenant get distinct cached tokens, and
// audiences flip the cache for hubs that mediate multiple downstreams.
//
// Token TTLs from the IdP minus 30s drive cache eviction. 4xx responses
// are not retried (configuration is wrong); 5xx responses are retried once
// with jitter to absorb transient IdP outages.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// GrantTypeTokenExchange is RFC 8693's grant_type value.
const GrantTypeTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"

// SubjectTokenTypeJWT identifies the incoming JWT as the subject token.
const SubjectTokenTypeJWT = "urn:ietf:params:oauth:token-type:jwt"

// RequestedTokenTypeAccessToken is the canonical "access_token" type IRI.
const RequestedTokenTypeAccessToken = "urn:ietf:params:oauth:token-type:access_token"

// ErrNoSubjectToken is returned when the dispatcher cannot supply the raw
// JWT (e.g. dev mode). Bubbles up as policy_denied: missing_subject_token.
var ErrNoSubjectToken = errors.New("oauth: subject token (raw JWT) is required")

// ExchangeError surfaces the IdP's error response. Callers branch on Code
// to decide whether to retry; AuditPayload renders a payload-safe summary
// for credential.exchange.failed events.
type ExchangeError struct {
	Status int    // HTTP status from the IdP
	Code   string // OAuth `error` field, when present
	Desc   string // OAuth `error_description`, when present
}

// Error renders the IdP error in a single line.
func (e *ExchangeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Desc != "" {
		return fmt.Sprintf("oauth: idp returned %d %s: %s", e.Status, e.Code, e.Desc)
	}
	if e.Code != "" {
		return fmt.Sprintf("oauth: idp returned %d %s", e.Status, e.Code)
	}
	return fmt.Sprintf("oauth: idp returned %d", e.Status)
}

// Retryable reports whether a fresh attempt is reasonable. 5xx is yes;
// 4xx is no (operator misconfiguration).
func (e *ExchangeError) Retryable() bool {
	return e != nil && e.Status >= 500
}

// AuditPayload returns a payload-safe summary for audit events. Never
// includes any token material.
func (e *ExchangeError) AuditPayload() map[string]any {
	if e == nil {
		return nil
	}
	out := map[string]any{"status": e.Status}
	if e.Code != "" {
		out["error_code"] = e.Code
	}
	if e.Desc != "" {
		// Truncate aggressively: error descriptions sometimes echo
		// payload material.
		desc := e.Desc
		if len(desc) > 200 {
			desc = desc[:200] + "…"
		}
		out["error_description"] = desc
	}
	return out
}

// Token is one exchange result. ExpiresAt is the absolute expiry minus
// the cache safety window so callers can treat "before ExpiresAt" as
// "still safe to use."
type Token struct {
	AccessToken string
	TokenType   string
	Scope       string
	ExpiresAt   time.Time
}

// ExchangeConfig configures the exchanger for one server. Exactly one
// config exists per server; the Exchanger dispatches by audience under
// the hood when a single Portico instance fronts multiple servers that
// share an IdP but require different audiences.
type ExchangeConfig struct {
	TokenURL     string        // POST endpoint
	ClientID     string        // OAuth client id
	ClientSecret string        // resolved from vault
	Audience     string        // RFC 8693 `audience`
	Scope        string        // optional space-separated scope list
	GrantType    string        // defaults to GrantTypeTokenExchange
	HTTPTimeout  time.Duration // request timeout (default 10s)
	SafetyWindow time.Duration // subtracted from expires_in (default 30s)
}

// Exchanger performs Token Exchange + caches results. Safe for concurrent
// callers across a fleet of incoming requests.
type Exchanger struct {
	cfg    ExchangeConfig
	http   *http.Client
	cache  *cache
	now    func() time.Time // injectable clock for tests
	logger func(format string, args ...any)
}

// New constructs an Exchanger. cfg.TokenURL and cfg.ClientID are required;
// callers passing http.Client = nil get a default client with cfg.HTTPTimeout.
func New(cfg ExchangeConfig, httpClient *http.Client) (*Exchanger, error) {
	if cfg.TokenURL == "" {
		return nil, errors.New("oauth: token url required")
	}
	if cfg.ClientID == "" {
		return nil, errors.New("oauth: client id required")
	}
	if cfg.GrantType == "" {
		cfg.GrantType = GrantTypeTokenExchange
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 10 * time.Second
	}
	if cfg.SafetyWindow <= 0 {
		cfg.SafetyWindow = 30 * time.Second
	}
	hc := httpClient
	if hc == nil {
		hc = &http.Client{Timeout: cfg.HTTPTimeout}
	}
	return &Exchanger{
		cfg:   cfg,
		http:  hc,
		cache: newCache(),
		now:   time.Now,
	}, nil
}

// WithClock allows tests to inject a deterministic clock. The returned
// pointer is the same exchanger; callers may chain.
func (e *Exchanger) WithClock(now func() time.Time) *Exchanger {
	if now != nil {
		e.now = now
	}
	return e
}

// Exchange returns a token usable as Authorization: Bearer for downstream
// requests. Cached on (tenant, user, audience). 5xx is retried once with
// jitter; 4xx is not retried.
func (e *Exchanger) Exchange(ctx context.Context, tenantID, userID, subjectToken string) (*Token, error) {
	if subjectToken == "" {
		return nil, ErrNoSubjectToken
	}

	key := tenantID + "\x00" + userID + "\x00" + e.cfg.Audience
	if tok, ok := e.cache.get(key, e.now); ok {
		return tok, nil
	}

	form := url.Values{}
	form.Set("grant_type", e.cfg.GrantType)
	form.Set("subject_token", subjectToken)
	form.Set("subject_token_type", SubjectTokenTypeJWT)
	form.Set("requested_token_type", RequestedTokenTypeAccessToken)
	if e.cfg.Audience != "" {
		form.Set("audience", e.cfg.Audience)
	}
	if e.cfg.Scope != "" {
		form.Set("scope", e.cfg.Scope)
	}
	if e.cfg.ClientSecret == "" {
		form.Set("client_id", e.cfg.ClientID)
	}

	tok, err := e.doExchange(ctx, form)
	if err != nil {
		var xe *ExchangeError
		if errors.As(err, &xe) && xe.Retryable() {
			// Single retry with jitter. Honor ctx cancellation while
			// waiting.
			delay := 200*time.Millisecond + time.Duration(rand.Int63n(int64(100*time.Millisecond)))
			t := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
				return nil, ctx.Err()
			case <-t.C:
			}
			tok, err = e.doExchange(ctx, form)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	e.cache.put(key, tok)
	return tok, nil
}

// doExchange performs a single POST to the IdP and returns either a Token
// (on 2xx) or an *ExchangeError / transport error.
func (e *Exchanger) doExchange(ctx context.Context, form url.Values) (*Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oauth: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if e.cfg.ClientSecret != "" {
		req.SetBasicAuth(e.cfg.ClientID, e.cfg.ClientSecret)
	}

	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: post token endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oauth: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		xe := &ExchangeError{Status: resp.StatusCode}
		// Best-effort JSON error parsing; tolerate empty / non-JSON
		// bodies (the spec allows non-conforming IdPs).
		if len(body) > 0 {
			var errBody struct {
				Code string `json:"error"`
				Desc string `json:"error_description"`
			}
			if jerr := json.Unmarshal(body, &errBody); jerr == nil {
				xe.Code = errBody.Code
				xe.Desc = errBody.Desc
			}
		}
		return nil, xe
	}

	var success struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	if err := json.Unmarshal(body, &success); err != nil {
		return nil, fmt.Errorf("oauth: decode token response: %w", err)
	}
	if success.AccessToken == "" {
		return nil, errors.New("oauth: idp returned empty access_token")
	}

	expSeconds := success.ExpiresIn
	if expSeconds <= 0 {
		expSeconds = int64((5 * time.Minute).Seconds())
	}
	expiresAt := e.now().Add(time.Duration(expSeconds) * time.Second).Add(-e.cfg.SafetyWindow)

	return &Token{
		AccessToken: success.AccessToken,
		TokenType:   success.TokenType,
		Scope:       success.Scope,
		ExpiresAt:   expiresAt,
	}, nil
}

// cache keys exchanges by (tenant, user, audience). Audience is constant
// for a given exchanger but using it in the key is cheap insurance against
// future per-call audience overrides.
type cache struct {
	mu sync.Mutex
	v  map[string]*Token
}

func newCache() *cache { return &cache{v: make(map[string]*Token)} }

// get returns the cached token if present and not expired (per the
// supplied clock). Expired entries are evicted lazily.
func (c *cache) get(key string, now func() time.Time) (*Token, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	tok, ok := c.v[key]
	if !ok {
		return nil, false
	}
	if now().After(tok.ExpiresAt) {
		delete(c.v, key)
		return nil, false
	}
	return tok, true
}

// put stores a token under key, replacing any prior entry.
func (c *cache) put(key string, t *Token) {
	if t == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.v[key] = t
}
