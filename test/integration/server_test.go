// Package integration ships cross-package integration tests that boot the
// HTTP server, exercise the full middleware chain, and assert end-to-end
// behaviour. Tests in this package import only public Portico interfaces
// and standard library / test helpers.
package integration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"

	pjwt "github.com/hurtener/Portico_gateway/internal/auth/jwt"
	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/server/api"
	"github.com/hurtener/Portico_gateway/internal/storage"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"

	// Self-register the sqlite driver.
	_ "github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

// testServer holds a running server + helpers for HTTP assertions.
type testServer struct {
	t        *testing.T
	srv      *httptest.Server
	tenants  ifaces.TenantStore
	priv     *rsa.PrivateKey
	jwksPath string
}

func (s *testServer) close() { s.srv.Close() }

func (s *testServer) get(path string, headers map[string]string) (*http.Response, []byte) {
	req, err := http.NewRequest(http.MethodGet, s.srv.URL+path, nil)
	if err != nil {
		s.t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, body
}

// signToken issues a JWT for a given tenant against this server's JWKS.
func (s *testServer) signToken(tenant string, scopes ...string) string {
	scope := strings.Join(scopes, " ")
	claims := jwtv5.MapClaims{
		"iss":    "https://test.local/",
		"aud":    []string{"portico"},
		"sub":    "tester",
		"tenant": tenant,
		"scope":  scope,
		"iat":    time.Now().Unix(),
		"exp":    time.Now().Add(time.Hour).Unix(),
	}
	tok := jwtv5.NewWithClaims(jwtv5.SigningMethodRS256, claims)
	tok.Header["kid"] = "k"
	signed, err := tok.SignedString(s.priv)
	if err != nil {
		s.t.Fatal(err)
	}
	return signed
}

// startProdServer boots a server with JWT auth (production-like).
func startProdServer(t *testing.T) *testServer {
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

	cfg := &config.Config{
		Server:  config.ServerConfig{Bind: "127.0.0.1:0"},
		Auth:    &config.AuthConfig{JWT: config.JWTConfig{Issuer: "https://test.local/", Audiences: []string{"portico"}, StaticJWKS: jwksPath}},
		Storage: config.StorageConfig{Driver: "sqlite", DSN: ":memory:"},
		Logging: config.LoggingConfig{Level: "error", Format: "json"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	backend, err := storage.Open(context.Background(), cfg.Storage, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	tenants := backend.Tenants()
	for _, id := range []string{"acme", "beta"} {
		if err := tenants.Upsert(context.Background(), &ifaces.Tenant{ID: id, DisplayName: id, Plan: "pro"}); err != nil {
			t.Fatal(err)
		}
	}

	v, err := pjwt.NewValidator(context.Background(), cfg.Auth.JWT)
	if err != nil {
		t.Fatal(err)
	}

	handler := api.NewRouter(api.Deps{
		Logger:    logger,
		Validator: v,
		Tenants:   tenants,
		Audit:     backend.Audit(),
		Version:   "test",
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &testServer{t: t, srv: srv, tenants: tenants, priv: priv, jwksPath: jwksPath}
}

func startDevServer(t *testing.T) *testServer {
	t.Helper()
	cfg := &config.Config{
		Server:  config.ServerConfig{Bind: "127.0.0.1:0"},
		Storage: config.StorageConfig{Driver: "sqlite", DSN: ":memory:"},
		Logging: config.LoggingConfig{Level: "error", Format: "json"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	backend, err := storage.Open(context.Background(), cfg.Storage, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	handler := api.NewRouter(api.Deps{
		Logger:    logger,
		DevMode:   true,
		DevTenant: "dev",
		Tenants:   backend.Tenants(),
		Audit:     backend.Audit(),
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &testServer{t: t, srv: srv, tenants: backend.Tenants()}
}

// ---------------- tests ----------------

func TestServerHealthz_DevMode(t *testing.T) {
	s := startDevServer(t)
	resp, body := s.get("/healthz", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), `"ok"`) {
		t.Errorf("body = %s", body)
	}
}

func TestServer_DevMode_AuditEndpoint_NoAuthRequired(t *testing.T) {
	s := startDevServer(t)
	resp, body := s.get("/v1/audit/events", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body=%s", resp.StatusCode, body)
	}
	var got struct {
		Events []any  `json:"events"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body not JSON: %s", body)
	}
	if got.Events == nil {
		t.Errorf("events should be empty array, not nil")
	}
}

func TestServer_DevMode_DevTenantAutoCreated(t *testing.T) {
	s := startDevServer(t)
	// Trigger the middleware via any authed endpoint
	resp, _ := s.get("/v1/audit/events", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if _, err := s.tenants.Get(context.Background(), "dev"); err != nil {
		t.Errorf("dev tenant not created: %v", err)
	}
}

func TestServer_AuditTenantScoping(t *testing.T) {
	s := startProdServer(t)

	tokAcme := s.signToken("acme", "user")
	tokBeta := s.signToken("beta", "user")

	resp, _ := s.get("/v1/audit/events", map[string]string{
		"Authorization": "Bearer " + tokAcme,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("acme: status = %d", resp.StatusCode)
	}

	resp, _ = s.get("/v1/audit/events", map[string]string{
		"Authorization": "Bearer " + tokBeta,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("beta: status = %d", resp.StatusCode)
	}

	resp, _ = s.get("/v1/audit/events", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", resp.StatusCode)
	}
}

func TestServer_AdminScopeRequired(t *testing.T) {
	s := startProdServer(t)

	tokUser := s.signToken("acme", "user")
	tokAdmin := s.signToken("acme", "user", "admin")

	resp, _ := s.get("/v1/admin/tenants", map[string]string{"Authorization": "Bearer " + tokUser})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("user: status = %d, want 403", resp.StatusCode)
	}

	resp, body := s.get("/v1/admin/tenants", map[string]string{"Authorization": "Bearer " + tokAdmin})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin: status = %d body=%s", resp.StatusCode, body)
	}
	var ts []ifaces.Tenant
	if err := json.Unmarshal(body, &ts); err != nil {
		t.Fatalf("admin body not JSON: %s", body)
	}
	if len(ts) < 2 {
		t.Errorf("expected >= 2 tenants, got %d", len(ts))
	}
}

func TestServer_NotFoundReturnsJSON(t *testing.T) {
	s := startDevServer(t)
	resp, body := s.get("/v1/does-not-exist", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var b map[string]any
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatalf("404 body not JSON: %s", body)
	}
	if b["error"] != "not_found" {
		t.Errorf("404 .error = %v", b["error"])
	}
}

func TestServer_ConsoleHomeRenders(t *testing.T) {
	s := startDevServer(t)
	resp, body := s.get("/", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "Portico Console") {
		t.Errorf("home didn't include 'Portico Console'; got: %s", body)
	}
}

func TestServer_StaticAssetServed(t *testing.T) {
	s := startDevServer(t)
	resp, body := s.get("/static/portico.css", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if len(body) == 0 {
		t.Error("css body empty")
	}
}
