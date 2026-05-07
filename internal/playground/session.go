// Package playground hosts the Phase 10 interactive MCP playground.
// It owns:
//
//   - A synthetic JWT minter (StartSession) that returns a short-lived
//     RS256 token bound to a tenant + scope set capped to read-only +
//     playground:execute. Never copies tenants:admin or secrets:write.
//   - A snapshot binder that can pin a session to a historical catalog
//     snapshot (Phase 6) so operators can rerun against the same surface
//     the original session saw.
//   - A correlation aggregator that collates spans (trace-lite from
//     audit), audit events, policy decisions, and drift events for one
//     in-flight call.
//   - The replay machinery that rehydrates a saved Case, opens a session,
//     re-issues the call, and records a Run row.
//
// The package is intentionally self-contained: the REST handlers under
// internal/server/api/playground*.go pull in the surface and route it
// through chi. cmd/portico/phase10_wiring.go hangs the playground off the
// real runtime objects (audit emitter, snapshot service, etc).
package playground

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
)

// SessionRequest captures the operator's intent: which tenant + actor +
// optional snapshot pin + runtime override.
type SessionRequest struct {
	TenantID        string
	ActorUserID     string
	SnapshotID      string   // "" → bind to current
	RuntimeOverride string   // "" | "shared_global" | "per_session"
	Scopes          []string // additional scopes the operator already holds (capped read-only)
}

// Session is the materialised playground session. Token is the synthetic
// JWT signed by the playground signing key.
type Session struct {
	ID         string
	TenantID   string
	ActorID    string
	SnapshotID string
	Token      string
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

// SigningKey is the RSA keypair used to sign synthetic playground JWTs.
// Construct via NewSigningKey; the playground caller registers the public
// half with the gateway's JWT validator (via JWKS document).
type SigningKey struct {
	Priv *rsa.PrivateKey
	Kid  string
}

// NewSigningKey generates a fresh RSA-2048 keypair for the playground.
// In production the gateway calls this once at boot; tests can use a
// fixed-seed variant if determinism is needed (out of scope here).
func NewSigningKey() (*SigningKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("playground: generate signing key: %w", err)
	}
	return &SigningKey{
		Priv: priv,
		Kid:  "playground-" + base64.RawURLEncoding.EncodeToString(randBytes(8)),
	}, nil
}

// JWKS returns a single-key JWKS document the gateway's JWT validator can
// consume as a static_jwks file. Callers also expose the same blob at a
// JWKS URL for remote loaders.
func (k *SigningKey) JWKS() map[string]any {
	pub := k.Priv.PublicKey
	return map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"kid": k.Kid,
			"use": "sig",
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString([]byte{0x01, 0x00, 0x01}),
		}},
	}
}

// Service mints sessions and is the public surface other packages depend
// on. Construct one per process; safe for concurrent use.
type Service struct {
	signing  *SigningKey
	issuer   string
	audience string
	ttl      time.Duration

	mu       sync.RWMutex
	sessions map[string]*Session
}

// Config is the constructor input.
type Config struct {
	SigningKey *SigningKey
	Issuer     string
	Audience   string
	TTL        time.Duration
}

// New constructs a Service.
func New(cfg Config) (*Service, error) {
	if cfg.SigningKey == nil {
		return nil, errors.New("playground: signing key required")
	}
	if cfg.Issuer == "" {
		cfg.Issuer = "portico-playground"
	}
	if cfg.Audience == "" {
		cfg.Audience = "portico"
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 30 * time.Minute
	}
	return &Service{
		signing:  cfg.SigningKey,
		issuer:   cfg.Issuer,
		audience: cfg.Audience,
		ttl:      cfg.TTL,
		sessions: make(map[string]*Session),
	}, nil
}

// Issuer reports the issuer string the gateway should add to its JWT
// validator's trust set.
func (s *Service) Issuer() string {
	if s == nil {
		return ""
	}
	return s.issuer
}

// SigningKey returns the signing key (the public half goes into the
// gateway's static JWKS).
func (s *Service) SigningKey() *SigningKey {
	if s == nil {
		return nil
	}
	return s.signing
}

// readOnlyScopeAllowlist enumerates scopes the playground is willing to
// embed into the synthetic token. Anything else is dropped — the
// playground must never escalate the operator's effective permissions.
var readOnlyScopeAllowlist = map[string]bool{
	"playground:execute": true,
	"playground:save":    true,
	// Read-only equivalents of the Phase 9 named scopes. The playground
	// needs to read servers/skills/snapshots; it does not need write.
	"servers:read":   true,
	"skills:read":    true,
	"policy:read":    true,
	"snapshots:read": true,
}

// capScopes drops anything not on the read-only allowlist and ensures
// playground:execute is present.
func capScopes(in []string) []string {
	out := []string{"playground:execute"}
	seen := map[string]bool{"playground:execute": true}
	for _, s := range in {
		if seen[s] {
			continue
		}
		if readOnlyScopeAllowlist[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out
}

// StartSession mints a fresh session token for the given request. The
// returned Session.Token is an RS256 JWT signed with the playground key.
func (s *Service) StartSession(_ context.Context, req SessionRequest) (*Session, error) {
	if s == nil {
		return nil, errors.New("playground: service not configured")
	}
	if req.TenantID == "" {
		return nil, errors.New("playground: tenant_id required")
	}
	if req.ActorUserID == "" {
		req.ActorUserID = "playground-anon"
	}
	now := time.Now().UTC()
	sid := "psn_" + ulid.MustNew(ulid.Timestamp(now), ulid.DefaultEntropy()).String()
	scopes := capScopes(req.Scopes)
	exp := now.Add(s.ttl)

	claims := jwtv5.MapClaims{
		"iss":    s.issuer,
		"aud":    []string{s.audience},
		"sub":    "playground:" + req.ActorUserID,
		"tenant": req.TenantID,
		"scope":  scopes,
		"iat":    now.Unix(),
		"exp":    exp.Unix(),
		"meta": map[string]any{
			"playground_session": sid,
			"snapshot_id":        req.SnapshotID,
			"runtime_override":   req.RuntimeOverride,
		},
	}
	tok := jwtv5.NewWithClaims(jwtv5.SigningMethodRS256, claims)
	tok.Header["kid"] = s.signing.Kid
	signed, err := tok.SignedString(s.signing.Priv)
	if err != nil {
		return nil, fmt.Errorf("playground: sign token: %w", err)
	}
	sess := &Session{
		ID:         sid,
		TenantID:   req.TenantID,
		ActorID:    req.ActorUserID,
		SnapshotID: req.SnapshotID,
		Token:      signed,
		ExpiresAt:  exp,
		CreatedAt:  now,
	}
	s.mu.Lock()
	s.sessions[sid] = sess
	s.mu.Unlock()
	return sess, nil
}

// Get returns the session with id sid, or nil if it doesn't exist /
// has expired.
func (s *Service) Get(sid string) *Session {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	sess, ok := s.sessions[sid]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	if time.Now().UTC().After(sess.ExpiresAt) {
		s.End(sid)
		return nil
	}
	return sess
}

// End deletes the session record. The signed token remains
// cryptographically valid until exp; callers that need real revocation
// should use a denylist (out of scope for the V1 playground).
func (s *Service) End(sid string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.sessions, sid)
	s.mu.Unlock()
}

// EndAll drops every session.
func (s *Service) EndAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.sessions = make(map[string]*Session)
	s.mu.Unlock()
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}
