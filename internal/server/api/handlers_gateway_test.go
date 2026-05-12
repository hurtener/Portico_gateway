package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayInfo_DevMode(t *testing.T) {
	d := Deps{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		DevMode:   true,
		DevTenant: "default",
		Version:   "v0.3.0",
		Gateway: GatewayInfo{
			Bind:    "127.0.0.1:8080",
			MCPPath: "/mcp",
		},
	}
	r := httptest.NewRequest(http.MethodGet, "/api/gateway/info", nil)
	w := httptest.NewRecorder()
	gatewayInfoHandler(d).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", w.Code)
	}
	var got gatewayInfoResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v: %s", err, w.Body.String())
	}
	if got.Bind != "127.0.0.1:8080" {
		t.Errorf("bind: got %q want %q", got.Bind, "127.0.0.1:8080")
	}
	if got.MCPPath != "/mcp" {
		t.Errorf("mcp_path: got %q want %q", got.MCPPath, "/mcp")
	}
	if !got.DevMode {
		t.Errorf("dev_mode: got false want true")
	}
	if got.DevTenant != "default" {
		t.Errorf("dev_tenant: got %q want %q", got.DevTenant, "default")
	}
	if got.Auth.Mode != "dev" {
		t.Errorf("auth.mode: got %q want %q", got.Auth.Mode, "dev")
	}
	if got.Auth.Issuer != "" {
		t.Errorf("auth.issuer: got %q want empty in dev mode", got.Auth.Issuer)
	}
}

func TestGatewayInfo_JWTMode(t *testing.T) {
	d := Deps{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		DevMode: false,
		Version: "v0.3.0",
		Gateway: GatewayInfo{
			Bind:           "0.0.0.0:8080",
			MCPPath:        "/mcp",
			JWTIssuer:      "https://issuer.example/",
			JWTAudiences:   []string{"portico"},
			JWTJWKSURL:     "https://issuer.example/.well-known/jwks.json",
			JWTTenantClaim: "tenant",
			JWTScopeClaim:  "scope",
		},
	}
	r := httptest.NewRequest(http.MethodGet, "/api/gateway/info", nil)
	w := httptest.NewRecorder()
	gatewayInfoHandler(d).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", w.Code)
	}
	var got gatewayInfoResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Auth.Mode != "jwt" {
		t.Errorf("auth.mode: got %q want %q", got.Auth.Mode, "jwt")
	}
	if got.Auth.Issuer != "https://issuer.example/" {
		t.Errorf("auth.issuer: got %q", got.Auth.Issuer)
	}
	if got.Auth.JWKSURL != "https://issuer.example/.well-known/jwks.json" {
		t.Errorf("auth.jwks_url: got %q", got.Auth.JWKSURL)
	}
	if got.Auth.TenantClaim != "tenant" {
		t.Errorf("auth.tenant_claim: got %q", got.Auth.TenantClaim)
	}
	if got.DevMode {
		t.Errorf("dev_mode: got true want false")
	}
}
