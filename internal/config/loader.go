package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Default values applied during Validate when the operator omits a field.
const (
	DefaultShutdownGrace = 10 * time.Second
	DefaultClockSkew     = 60 * time.Second
	DefaultTenantClaim   = "tenant"
	DefaultScopeClaim    = "scope"
)

// tenantIDRegexp matches the allowed tenant ID format. Lowercase only,
// alphanumeric plus underscore/dash, 1-64 chars.
var tenantIDRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// serverIDRegexp matches the allowed server ID format. Same shape as the
// namespace package's regexp (kept in sync) so the namespace JoinTool/
// SplitTool always sees a well-formed prefix.
var serverIDRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// Load parses + validates a portico.yaml from disk.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-supplied path; this is the entire purpose
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	return Parse(raw)
}

// Parse parses + validates a YAML byte slice. Used by tests and by Load.
func Parse(raw []byte) (*Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(false) // tolerate unknown keys for forward-compat
	if err := dec.Decode(&cfg); err != nil {
		// io.EOF on empty input is fine — return zero config (will fail Validate)
		if !errors.Is(err, fmt.Errorf("EOF")) && err.Error() != "EOF" {
			return nil, fmt.Errorf("config: parse: %w", err)
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate applies defaults and enforces invariants. Mutates the receiver.
// Linter note: deliberately a flat sequence of field checks so config
// errors point at the first offending field rather than nested helpers.
//
//nolint:gocyclo
func (c *Config) Validate() error {
	// Server
	if c.Server.Bind == "" {
		return fieldErr("server.bind", "is required")
	}
	if c.Server.ShutdownGrace == 0 {
		c.Server.ShutdownGrace = DefaultShutdownGrace
	}

	// Auth / dev-mode safety
	devMode := c.IsDevMode()
	if c.Auth == nil && !devMode {
		return fieldErr("auth", "is required when server.bind is not localhost (dev mode requires 127.0.0.1 / localhost bind)")
	}
	if c.Auth != nil {
		if err := c.Auth.JWT.applyDefaultsAndValidate(); err != nil {
			return err
		}
	}

	// Storage
	if c.Storage.Driver == "" {
		c.Storage.Driver = "sqlite"
	}
	if c.Storage.Driver != "sqlite" {
		return fieldErr("storage.driver", "only 'sqlite' is supported in V1")
	}
	if c.Storage.DSN == "" {
		c.Storage.DSN = "file:./portico.db?cache=shared"
	}

	// Tenants
	seen := make(map[string]struct{})
	for i, t := range c.Tenants {
		if t.ID == "" {
			return fieldErr(fmt.Sprintf("tenants[%d].id", i), "is required")
		}
		if !tenantIDRegexp.MatchString(t.ID) {
			return fieldErr(fmt.Sprintf("tenants[%d].id", i), fmt.Sprintf("invalid format %q (must match %s)", t.ID, tenantIDRegexp.String()))
		}
		if _, dup := seen[t.ID]; dup {
			return fieldErr(fmt.Sprintf("tenants[%d].id", i), fmt.Sprintf("duplicate tenant id %q", t.ID))
		}
		seen[t.ID] = struct{}{}
	}

	// Servers — Phase 1 validates id shape, transport, and that the
	// transport-specific block is populated. Phase 2 extends this with
	// runtime_mode + lifecycle defaults.
	seenServer := make(map[string]struct{})
	for i, s := range c.Servers {
		if s.ID == "" {
			return fieldErr(fmt.Sprintf("servers[%d].id", i), "is required")
		}
		if !serverIDRegexp.MatchString(s.ID) {
			return fieldErr(fmt.Sprintf("servers[%d].id", i),
				fmt.Sprintf("invalid format %q (must match %s)", s.ID, serverIDRegexp.String()))
		}
		if _, dup := seenServer[s.ID]; dup {
			return fieldErr(fmt.Sprintf("servers[%d].id", i),
				fmt.Sprintf("duplicate server id %q", s.ID))
		}
		seenServer[s.ID] = struct{}{}
		switch s.Transport {
		case "stdio":
			if s.Stdio == nil || s.Stdio.Command == "" {
				return fieldErr(fmt.Sprintf("servers[%d].stdio.command", i),
					"is required when transport=stdio")
			}
		case "http":
			if s.HTTP == nil || s.HTTP.URL == "" {
				return fieldErr(fmt.Sprintf("servers[%d].http.url", i),
					"is required when transport=http")
			}
		case "":
			return fieldErr(fmt.Sprintf("servers[%d].transport", i), "is required")
		default:
			return fieldErr(fmt.Sprintf("servers[%d].transport", i),
				fmt.Sprintf("unsupported transport %q", s.Transport))
		}
	}

	// Logging
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "json"
	}

	return nil
}

// applyDefaultsAndValidate fills JWT defaults in place and rejects bad config.
func (j *JWTConfig) applyDefaultsAndValidate() error {
	if j.Issuer == "" {
		return fieldErr("auth.jwt.issuer", "is required")
	}
	if j.JWKSURL == "" && j.StaticJWKS == "" {
		return fieldErr("auth.jwt", "either jwks_url or static_jwks is required")
	}
	if j.JWKSURL != "" && j.StaticJWKS != "" {
		return fieldErr("auth.jwt", "jwks_url and static_jwks are mutually exclusive")
	}
	if j.TenantClaim == "" {
		j.TenantClaim = DefaultTenantClaim
	}
	if j.ScopeClaim == "" {
		j.ScopeClaim = DefaultScopeClaim
	}
	if j.ClockSkew == 0 {
		j.ClockSkew = DefaultClockSkew
	}
	return nil
}

// IsDevMode reports whether the server is in dev mode: no auth configured AND
// listener bound to localhost. The localhost requirement prevents accidental
// open-network deployments without auth.
func (c *Config) IsDevMode() bool {
	if c.Auth != nil {
		return false
	}
	bind := strings.ToLower(c.Server.Bind)
	host, _ := splitHostPort(bind)
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return true
	default:
		return false
	}
}

// splitHostPort tolerates inputs like "127.0.0.1:8080", "[::1]:8080", "localhost:0".
func splitHostPort(s string) (host, port string) {
	if s == "" {
		return "", ""
	}
	// IPv6 bracketed form
	if strings.HasPrefix(s, "[") {
		if end := strings.Index(s, "]"); end > 0 {
			host = s[1:end]
			if end+1 < len(s) && s[end+1] == ':' {
				port = s[end+2:]
			}
			return
		}
	}
	if i := strings.LastIndex(s, ":"); i > 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// FieldError describes a config validation failure with a JSON-pointer-ish path.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("config: %s: %s", e.Field, e.Message)
}

func fieldErr(field, msg string) error {
	return &FieldError{Field: field, Message: msg}
}
