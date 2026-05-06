package tenant_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"

	pjwt "github.com/hurtener/Portico_gateway/internal/auth/jwt"
	tenantmw "github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// fakeTenantStore implements ifaces.TenantStore for middleware tests.
type fakeTenantStore struct {
	tenants map[string]*ifaces.Tenant
}

func newFakeStore(ids ...string) *fakeTenantStore {
	s := &fakeTenantStore{tenants: map[string]*ifaces.Tenant{}}
	for _, id := range ids {
		s.tenants[id] = &ifaces.Tenant{ID: id, DisplayName: id, Plan: "pro"}
	}
	return s
}

func (s *fakeTenantStore) Get(_ context.Context, id string) (*ifaces.Tenant, error) {
	t, ok := s.tenants[id]
	if !ok {
		return nil, ifaces.ErrNotFound
	}
	return t, nil
}
func (s *fakeTenantStore) List(_ context.Context) ([]*ifaces.Tenant, error) {
	out := make([]*ifaces.Tenant, 0, len(s.tenants))
	for _, t := range s.tenants {
		out = append(out, t)
	}
	return out, nil
}
func (s *fakeTenantStore) Upsert(_ context.Context, t *ifaces.Tenant) error {
	s.tenants[t.ID] = t
	return nil
}
func (s *fakeTenantStore) Delete(_ context.Context, id string) error {
	delete(s.tenants, id)
	return nil
}

// makeJWT signs a JWT with the supplied tenant claim using a fresh keypair
// and returns (token, jwks-path).
func makeJWT(t *testing.T, tenant string) (string, string, *rsa.PrivateKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks := map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA", "kid": "k", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes()),
			"e": "AQAB",
		}},
	}
	dir := t.TempDir()
	jwksPath := filepath.Join(dir, "jwks.json")
	body, _ := json.Marshal(jwks)
	if err := os.WriteFile(jwksPath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	claims := jwtv5.MapClaims{
		"iss":    "https://test.local/",
		"aud":    []string{"portico"},
		"sub":    "u1",
		"tenant": tenant,
		"scope":  "user",
		"iat":    time.Now().Unix(),
		"exp":    time.Now().Add(time.Hour).Unix(),
	}
	tok := jwtv5.NewWithClaims(jwtv5.SigningMethodRS256, claims)
	tok.Header["kid"] = "k"
	signed, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signed, jwksPath, priv
}

func TestMiddleware_DevModeBypass(t *testing.T) {
	store := newFakeStore()
	mw := tenantmw.Middleware(tenantmw.MiddlewareConfig{
		DevMode:     true,
		DevTenant:   "dev",
		TenantStore: store,
	})

	called := false
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		id, ok := tenantmw.From(r.Context())
		if !ok {
			t.Error("expected identity in context")
		}
		if id.TenantID != "dev" {
			t.Errorf("tenant = %q, want dev", id.TenantID)
		}
		if !id.DevMode {
			t.Error("DevMode flag not set")
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/audit/events", nil)
	h.ServeHTTP(rec, req)
	if !called {
		t.Fatal("handler not invoked")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
	// Dev tenant should be auto-upserted into the store
	if _, err := store.Get(context.Background(), "dev"); err != nil {
		t.Errorf("dev tenant not auto-created in store: %v", err)
	}
}

func TestMiddleware_DevModeOverrideEnv(t *testing.T) {
	t.Setenv("PORTICO_DEV_TENANT", "alpha")
	store := newFakeStore()
	mw := tenantmw.Middleware(tenantmw.MiddlewareConfig{
		DevMode:     true,
		TenantStore: store,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/audit/events", nil)
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := tenantmw.From(r.Context())
		if id.TenantID != "alpha" {
			t.Errorf("tenant = %q, want alpha", id.TenantID)
		}
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)
}

func TestMiddleware_ProductionRequiresJWT(t *testing.T) {
	signed, jwksPath, _ := makeJWT(t, "acme")
	v, err := pjwt.NewValidator(context.Background(), config.JWTConfig{
		Issuer: "https://test.local/", Audiences: []string{"portico"}, StaticJWKS: jwksPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := newFakeStore("acme")
	mw := tenantmw.Middleware(tenantmw.MiddlewareConfig{
		Validator:   v,
		TenantStore: store,
	})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	t.Run("no header -> 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/audit/events", nil)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Error("missing WWW-Authenticate header")
		}
	})

	t.Run("valid jwt -> 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/audit/events", nil)
		req.Header.Set("Authorization", "Bearer "+signed)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})
}

func TestMiddleware_UnknownTenant(t *testing.T) {
	signed, jwksPath, _ := makeJWT(t, "ghost")
	v, _ := pjwt.NewValidator(context.Background(), config.JWTConfig{
		Issuer: "https://test.local/", Audiences: []string{"portico"}, StaticJWKS: jwksPath,
	})
	store := newFakeStore() // empty
	mw := tenantmw.Middleware(tenantmw.MiddlewareConfig{
		Validator: v, TenantStore: store,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/audit/events", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be invoked")
	})).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestMiddleware_HealthzAlwaysAllowed(t *testing.T) {
	store := newFakeStore("acme")
	v, _ := pjwt.NewValidator(context.Background(), config.JWTConfig{
		Issuer: "https://test.local/", Audiences: []string{"portico"},
		StaticJWKS: writeStaticJWKS(t),
	})
	mw := tenantmw.Middleware(tenantmw.MiddlewareConfig{
		Validator: v, TenantStore: store,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestMiddleware_StaticAssetsAllowed(t *testing.T) {
	store := newFakeStore("acme")
	v, _ := pjwt.NewValidator(context.Background(), config.JWTConfig{
		Issuer: "https://test.local/", Audiences: []string{"portico"},
		StaticJWKS: writeStaticJWKS(t),
	})
	mw := tenantmw.Middleware(tenantmw.MiddlewareConfig{
		Validator: v, TenantStore: store,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/_app/immutable/start.js", nil)
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func writeStaticJWKS(t *testing.T) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA", "kid": "k", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes()),
			"e": "AQAB",
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
