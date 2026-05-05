package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/config"
)

func TestLoad_ValidConfig(t *testing.T) {
	cfg, err := config.Load("../../testdata/portico.yaml")
	if err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
	if got, want := len(cfg.Tenants), 2; got != want {
		t.Fatalf("tenants: got %d want %d", got, want)
	}
	if cfg.IsDevMode() {
		t.Fatalf("config should NOT be in dev mode (auth is set)")
	}
}

func TestLoad_BadYAML(t *testing.T) {
	_, err := config.Load("../../testdata/portico-bad.yaml")
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}
	var fe *config.FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *config.FieldError, got %T: %v", err, err)
	}
	if fe.Field != "server.bind" {
		t.Fatalf("expected error on server.bind, got %q", fe.Field)
	}
}

func TestValidate_DuplicateTenantID(t *testing.T) {
	yaml := []byte(`
server:
  bind: 127.0.0.1:8080
tenants:
  - id: acme
    display_name: A
    plan: pro
  - id: acme
    display_name: A2
    plan: enterprise
`)
	_, err := config.Parse(yaml)
	if err == nil {
		t.Fatalf("expected duplicate tenant id error")
	}
}

func TestValidate_BadTenantID(t *testing.T) {
	yaml := []byte(`
server:
  bind: 127.0.0.1:8080
tenants:
  - id: "Bad ID"
    display_name: x
    plan: pro
`)
	_, err := config.Parse(yaml)
	if err == nil {
		t.Fatalf("expected bad tenant id error")
	}
}

func TestValidate_AuthRequiredOutsideDev(t *testing.T) {
	yaml := []byte(`
server:
  bind: 0.0.0.0:8080
`)
	_, err := config.Parse(yaml)
	if err == nil {
		t.Fatalf("expected auth-required error")
	}
}

func TestIsDevMode(t *testing.T) {
	cases := []struct {
		name string
		bind string
		auth bool
		want bool
	}{
		{"localhost ipv4 no auth", "127.0.0.1:8080", false, true},
		{"localhost ipv6 no auth", "[::1]:8080", false, true},
		{"localhost name no auth", "localhost:8080", false, true},
		{"public no auth fails validate", "0.0.0.0:8080", false, false}, // validate would error; IsDevMode would return false
		{"localhost with auth", "127.0.0.1:8080", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &config.Config{
				Server:  config.ServerConfig{Bind: tc.bind},
				Storage: config.StorageConfig{Driver: "sqlite", DSN: ":memory:"},
				Logging: config.LoggingConfig{Level: "info", Format: "json"},
			}
			if tc.auth {
				c.Auth = &config.AuthConfig{
					JWT: config.JWTConfig{
						Issuer:     "https://test.local/",
						StaticJWKS: "ignored",
					},
				}
			}
			if got := c.IsDevMode(); got != tc.want {
				t.Fatalf("IsDevMode = %v, want %v", got, tc.want)
			}
		})
	}
}

// helper used by other test packages that need a fresh-on-disk valid config.
func writeTempYAML(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "portico.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParse_DefaultsApplied(t *testing.T) {
	cfg, err := config.Parse([]byte(`
server:
  bind: 127.0.0.1:8080
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.ShutdownGrace == 0 {
		t.Errorf("ShutdownGrace default not applied")
	}
	if cfg.Storage.Driver != "sqlite" {
		t.Errorf("Storage.Driver default not applied: %q", cfg.Storage.Driver)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level default not applied: %q", cfg.Logging.Level)
	}
}

var _ = writeTempYAML // silence "unused" if other tests are skipped
