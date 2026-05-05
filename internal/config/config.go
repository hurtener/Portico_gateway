// Package config defines the YAML schema for portico.yaml plus loader and validator.
//
// Phase 0 ships the full V1 type surface; later phases reference these types
// without redefining them. Hot-reloadable fields are documented per-section.
package config

import "time"

// Config is the top-level shape of portico.yaml.
type Config struct {
	Server  ServerConfig   `yaml:"server"`
	Auth    *AuthConfig    `yaml:"auth,omitempty"` // nil => dev mode (must be combined with localhost bind)
	Storage StorageConfig  `yaml:"storage"`
	Tenants []TenantConfig `yaml:"tenants"`
	Skills  SkillsConfig   `yaml:"skills"`
	Logging LoggingConfig  `yaml:"logging"`
	// Servers is consumed by Phase 1+. Phase 0 parses but does not act on it.
	Servers []ServerSpec `yaml:"servers,omitempty"`
}

// ServerConfig governs the HTTP listener.
type ServerConfig struct {
	Bind          string        `yaml:"bind"`           // e.g. "127.0.0.1:8080"
	ShutdownGrace time.Duration `yaml:"shutdown_grace"` // default 10s
}

// AuthConfig groups all authentication strategy configuration. Phase 0 ships JWT only.
type AuthConfig struct {
	JWT JWTConfig `yaml:"jwt"`
}

// JWTConfig configures the bearer-token validator.
type JWTConfig struct {
	Issuer        string        `yaml:"issuer"`
	Audiences     []string      `yaml:"audiences"`
	JWKSURL       string        `yaml:"jwks_url,omitempty"`
	StaticJWKS    string        `yaml:"static_jwks,omitempty"`    // path to local JWKS file
	TenantClaim   string        `yaml:"tenant_claim,omitempty"`   // default "tenant"
	ScopeClaim    string        `yaml:"scope_claim,omitempty"`    // default "scope"
	RequiredScope string        `yaml:"required_scope,omitempty"` // optional global scope check
	ClockSkew     time.Duration `yaml:"clock_skew,omitempty"`     // default 60s
}

// StorageConfig selects the persistence backend.
type StorageConfig struct {
	Driver string `yaml:"driver"` // "sqlite" only in Phase 0
	DSN    string `yaml:"dsn"`    // e.g. "file:./portico.db?cache=shared"
}

// TenantConfig declares one tenant. The synthetic dev tenant is materialized
// implicitly when dev mode is active and no tenants are listed.
type TenantConfig struct {
	ID             string            `yaml:"id"`
	DisplayName    string            `yaml:"display_name"`
	Plan           string            `yaml:"plan"` // free | pro | enterprise (or operator-defined)
	CredentialsRef string            `yaml:"credentials_ref,omitempty"`
	Entitlements   Entitlements      `yaml:"entitlements"`
	Metadata       map[string]string `yaml:"metadata,omitempty"`
}

// Entitlements gates skills + capacity at the tenant level.
type Entitlements struct {
	Skills      []string `yaml:"skills"` // glob patterns: "github.*", "*"
	MaxSessions int      `yaml:"max_sessions"`
}

// SkillsConfig groups skill-source declarations.
type SkillsConfig struct {
	Sources []SkillSourceConfig `yaml:"sources"`
}

// SkillSourceConfig declares one skill source. Phase 4 wires this; Phase 0 parses.
type SkillSourceConfig struct {
	Type string `yaml:"type"` // "local" in V1
	Path string `yaml:"path,omitempty"`
}

// LoggingConfig controls the global logger.
type LoggingConfig struct {
	Level  string `yaml:"level"`  // debug | info | warn | error
	Format string `yaml:"format"` // json | text
}

// ServerSpec is the schema for a registered MCP server. Phase 1 wires the
// minimum needed to instantiate stdio/http southbound clients; Phase 2 layers
// per-runtime-mode lifecycle, hot reload, and dynamic CRUD on top.
type ServerSpec struct {
	ID          string     `yaml:"id"`
	DisplayName string     `yaml:"display_name,omitempty"`
	Transport   string     `yaml:"transport"` // stdio | http
	RuntimeMode string     `yaml:"runtime_mode,omitempty"`
	Stdio       *StdioSpec `yaml:"stdio,omitempty"`
	HTTP        *HTTPSpec  `yaml:"http,omitempty"`
	// StartTimeout is the southbound-handshake budget (initialize round-trip).
	StartTimeout time.Duration `yaml:"start_timeout,omitempty"`
}

// StdioSpec configures a stdio-transport downstream MCP server.
type StdioSpec struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args,omitempty"`
	Env     []string `yaml:"env,omitempty"` // KEY=VALUE pairs
	Cwd     string   `yaml:"cwd,omitempty"`
}

// HTTPSpec configures an HTTP-transport downstream MCP server.
type HTTPSpec struct {
	URL        string        `yaml:"url"`
	AuthHeader string        `yaml:"auth_header,omitempty"` // Phase 5 wires real values from vault
	Timeout    time.Duration `yaml:"timeout,omitempty"`
}
