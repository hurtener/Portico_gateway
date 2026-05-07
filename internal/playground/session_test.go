package playground

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	key, err := NewSigningKey()
	if err != nil {
		t.Fatalf("signing key: %v", err)
	}
	svc, err := New(Config{SigningKey: key, Issuer: "test", Audience: "portico"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return svc
}

func TestStartSession_MintsScopedJWT(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.StartSession(context.Background(), SessionRequest{
		TenantID:    "tenant-a",
		ActorUserID: "alice",
		Scopes:      []string{"servers:read", "secrets:write" /* should be dropped */, "admin" /* should be dropped */},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if sess.Token == "" {
		t.Fatalf("expected non-empty token")
	}
	parts := strings.Split(sess.Token, ".")
	if len(parts) != 3 {
		t.Fatalf("not a 3-part JWT")
	}
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	_ = json.Unmarshal(payload, &claims)
	if claims["tenant"] != "tenant-a" {
		t.Fatalf("tenant claim: %v", claims["tenant"])
	}
	scopes, _ := claims["scope"].([]any)
	if len(scopes) == 0 {
		t.Fatalf("expected scopes in claim")
	}
	hasExecute, hasAdmin := false, false
	for _, s := range scopes {
		if s == "playground:execute" {
			hasExecute = true
		}
		if s == "admin" || s == "secrets:write" {
			hasAdmin = true
		}
	}
	if !hasExecute {
		t.Fatalf("expected playground:execute scope")
	}
	if hasAdmin {
		t.Fatalf("playground token must not embed admin / secrets:write")
	}

	// Verify alg is RS256.
	header, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var hdr map[string]any
	_ = json.Unmarshal(header, &hdr)
	if hdr["alg"] != "RS256" {
		t.Fatalf("alg should be RS256, got %v", hdr["alg"])
	}
}

func TestStartSession_RejectsExpired(t *testing.T) {
	key, err := NewSigningKey()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	svc, err := New(Config{SigningKey: key, TTL: time.Millisecond})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	sess, err := svc.StartSession(context.Background(), SessionRequest{TenantID: "t"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if got := svc.Get(sess.ID); got != nil {
		t.Fatalf("expected expired session to be reaped, got %+v", got)
	}
}

func TestStartSession_RuntimeOverride_AdminOnly(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.StartSession(context.Background(), SessionRequest{
		TenantID:        "tenant-a",
		ActorUserID:     "admin",
		RuntimeOverride: "per_session",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	parts := strings.Split(sess.Token, ".")
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims jwtv5.MapClaims
	_ = json.Unmarshal(payload, &claims)
	meta, _ := claims["meta"].(map[string]any)
	if meta == nil || meta["runtime_override"] != "per_session" {
		t.Fatalf("expected runtime_override in meta")
	}
}

func TestStartSession_RequiresTenant(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.StartSession(context.Background(), SessionRequest{}); err == nil {
		t.Fatalf("expected error for empty tenant")
	}
}

func TestService_LifecycleAndAccessors(t *testing.T) {
	key, _ := NewSigningKey()
	svc, err := New(Config{SigningKey: key})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if svc.Issuer() != "portico-playground" {
		t.Fatalf("default issuer wrong: %s", svc.Issuer())
	}
	if svc.SigningKey() == nil {
		t.Fatalf("signing key should be visible")
	}
	jwks := key.JWKS()
	keys, _ := jwks["keys"].([]map[string]any)
	if len(keys) != 1 || keys[0]["kty"] != "RSA" {
		t.Fatalf("jwks shape wrong: %+v", jwks)
	}

	sess, err := svc.StartSession(context.Background(), SessionRequest{TenantID: "t"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := svc.Get(sess.ID); got == nil || got.ID != sess.ID {
		t.Fatalf("Get round trip failed")
	}
	svc.End(sess.ID)
	if got := svc.Get(sess.ID); got != nil {
		t.Fatalf("expected session to be gone after End")
	}

	// EndAll wipes any remaining sessions.
	_, _ = svc.StartSession(context.Background(), SessionRequest{TenantID: "t"})
	svc.EndAll()
}

func TestService_NilSafety(t *testing.T) {
	var svc *Service
	if svc.Issuer() != "" {
		t.Fatalf("nil service issuer should be empty")
	}
	if svc.SigningKey() != nil {
		t.Fatalf("nil service signing key should be nil")
	}
	if svc.Get("x") != nil {
		t.Fatalf("nil service Get should be nil")
	}
	svc.End("x")
	svc.EndAll()
	if _, err := svc.StartSession(context.Background(), SessionRequest{TenantID: "t"}); err == nil {
		t.Fatalf("expected error on nil StartSession")
	}
}

func TestNew_RequiresSigningKey(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatalf("expected error without signing key")
	}
}
