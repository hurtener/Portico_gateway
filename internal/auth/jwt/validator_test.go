package jwt_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"

	pjwt "github.com/hurtener/Portico_gateway/internal/auth/jwt"
	"github.com/hurtener/Portico_gateway/internal/config"
)

// testKey holds an RSA keypair plus its kid for signing test tokens.
type testKey struct {
	priv *rsa.PrivateKey
	kid  string
}

func newTestKey(t *testing.T) *testKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &testKey{priv: priv, kid: "test-key-1"}
}

// writeJWKS dumps a JWKS document containing the public key to a temp file
// and returns its path.
func (k *testKey) writeJWKS(t *testing.T) string {
	t.Helper()
	doc := map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"kid": k.kid,
			"use": "sig",
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(k.priv.PublicKey.N.Bytes()),
			"e":   "AQAB",
		}},
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "jwks.json")
	body, _ := json.Marshal(doc)
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// signClaims builds + signs an RS256 JWT.
func (k *testKey) signClaims(t *testing.T, c jwtv5.MapClaims) string {
	t.Helper()
	tok := jwtv5.NewWithClaims(jwtv5.SigningMethodRS256, c)
	tok.Header["kid"] = k.kid
	signed, err := tok.SignedString(k.priv)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func defaultClaims(now time.Time) jwtv5.MapClaims {
	return jwtv5.MapClaims{
		"iss":    "https://test.local/",
		"aud":    []string{"portico"},
		"sub":    "alice@acme",
		"tenant": "acme",
		"scope":  "user admin",
		"iat":    now.Unix(),
		"exp":    now.Add(time.Hour).Unix(),
	}
}

func newValidator(t *testing.T, cfg config.JWTConfig) *pjwt.Validator {
	t.Helper()
	v, err := pjwt.NewValidator(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestValidate_ValidRS256(t *testing.T) {
	k := newTestKey(t)
	jwks := k.writeJWKS(t)
	v := newValidator(t, config.JWTConfig{
		Issuer:     "https://test.local/",
		Audiences:  []string{"portico"},
		StaticJWKS: jwks,
	})
	tok := k.signClaims(t, defaultClaims(time.Now()))
	c, err := v.Validate(context.Background(), tok)
	if err != nil {
		t.Fatalf("expected valid token, err: %v", err)
	}
	if c.Tenant != "acme" {
		t.Errorf("tenant = %q want acme", c.Tenant)
	}
	if c.Subject != "alice@acme" {
		t.Errorf("subject mismatch: %q", c.Subject)
	}
	if !contains(c.Scopes, "admin") {
		t.Errorf("scopes missing admin: %v", c.Scopes)
	}
}

func TestValidate_ExpiredToken(t *testing.T) {
	k := newTestKey(t)
	v := newValidator(t, config.JWTConfig{
		Issuer:     "https://test.local/",
		Audiences:  []string{"portico"},
		StaticJWKS: k.writeJWKS(t),
		ClockSkew:  1 * time.Second,
	})
	c := defaultClaims(time.Now().Add(-2 * time.Hour))
	c["exp"] = time.Now().Add(-time.Hour).Unix()
	tok := k.signClaims(t, c)
	if _, err := v.Validate(context.Background(), tok); err == nil {
		t.Fatal("expected expired-token error, got nil")
	}
}

func TestValidate_BadSignature(t *testing.T) {
	k := newTestKey(t)
	other := newTestKey(t)
	other.kid = k.kid // same kid, different key -> signature won't verify
	v := newValidator(t, config.JWTConfig{
		Issuer:     "https://test.local/",
		Audiences:  []string{"portico"},
		StaticJWKS: k.writeJWKS(t),
	})
	tok := other.signClaims(t, defaultClaims(time.Now()))
	if _, err := v.Validate(context.Background(), tok); err == nil {
		t.Fatal("expected signature error")
	}
}

func TestValidate_RejectsHS256(t *testing.T) {
	k := newTestKey(t)
	jwks := k.writeJWKS(t)
	v := newValidator(t, config.JWTConfig{
		Issuer:     "https://test.local/",
		Audiences:  []string{"portico"},
		StaticJWKS: jwks,
	})
	// Sign with HS256 using a shared secret. Validator must reject the alg.
	c := defaultClaims(time.Now())
	tok := jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, c)
	tok.Header["kid"] = k.kid
	signed, err := tok.SignedString([]byte("a-shared-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Validate(context.Background(), signed); err == nil {
		t.Fatal("expected HS256 rejection")
	} else if !strings.Contains(err.Error(), "alg") && !strings.Contains(err.Error(), "method") {
		// Acceptable as long as the rejection is clear-cut; the assertion above
		// is the load-bearing one.
		t.Logf("non-fatal: rejection message did not mention alg: %v", err)
	}
}

func TestValidate_BadIssuer(t *testing.T) {
	k := newTestKey(t)
	v := newValidator(t, config.JWTConfig{
		Issuer:     "https://test.local/",
		Audiences:  []string{"portico"},
		StaticJWKS: k.writeJWKS(t),
	})
	c := defaultClaims(time.Now())
	c["iss"] = "https://attacker.example.com/"
	tok := k.signClaims(t, c)
	if _, err := v.Validate(context.Background(), tok); err == nil {
		t.Fatal("expected issuer mismatch error")
	}
}

func TestValidate_MissingTenantClaim(t *testing.T) {
	k := newTestKey(t)
	v := newValidator(t, config.JWTConfig{
		Issuer:     "https://test.local/",
		Audiences:  []string{"portico"},
		StaticJWKS: k.writeJWKS(t),
	})
	c := defaultClaims(time.Now())
	delete(c, "tenant")
	tok := k.signClaims(t, c)
	if _, err := v.Validate(context.Background(), tok); err == nil {
		t.Fatal("expected missing-tenant-claim error")
	} else if !strings.Contains(err.Error(), "tenant") {
		t.Errorf("error should mention tenant: %v", err)
	}
}

func TestValidate_ClockSkew(t *testing.T) {
	k := newTestKey(t)
	v := newValidator(t, config.JWTConfig{
		Issuer:     "https://test.local/",
		Audiences:  []string{"portico"},
		StaticJWKS: k.writeJWKS(t),
		ClockSkew:  60 * time.Second,
	})
	c := defaultClaims(time.Now())
	// Expired by 30s; with 60s skew, accepted.
	c["exp"] = time.Now().Add(-30 * time.Second).Unix()
	tok := k.signClaims(t, c)
	if _, err := v.Validate(context.Background(), tok); err != nil {
		t.Fatalf("expected acceptance within skew, got %v", err)
	}
}

// helpers

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// ensure fmt import used (errcheck of jwks helper structure)
var _ = fmt.Sprintf
