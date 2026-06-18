// Package config defines the YAML schema for portico.yaml plus loader and validator.
//
// Phase 0 ships the full V1 type surface; later phases reference these types
// without redefining them. Hot-reloadable fields are documented per-section.
package config

import "time"

// Config is the top-level shape of portico.yaml.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Auth      *AuthConfig     `yaml:"auth,omitempty"` // nil => dev mode (must be combined with localhost bind)
	Storage   StorageConfig   `yaml:"storage"`
	Tenants   []TenantConfig  `yaml:"tenants"`
	Skills    SkillsConfig    `yaml:"skills"`
	Logging   LoggingConfig   `yaml:"logging"`
	Telemetry TelemetryConfig `yaml:"telemetry,omitempty"`
	// Servers is consumed by Phase 1+. Phase 0 parses but does not act on it.
	Servers []ServerSpec `yaml:"servers,omitempty"`
	// CodeMode is the Phase 13.5 Code Mode posture. Zero value = fully permissive
	// (open within a tenant); operators tighten it.
	CodeMode CodeModeConfig `yaml:"code_mode,omitempty"`
	// AgentProfiles declares Agent Profiles (Phase 14) for cold-start. Each is
	// seeded into the tenant-scoped store at boot (idempotent by tenant+name).
	// Absent block => no profiles configured => every request gets the default
	// (full-surface) profile => V1/V1.5 behaviour unchanged.
	AgentProfiles []AgentProfileConfig `yaml:"agent_profiles,omitempty"`
	// Cache configures the Phase 15.5 semantic cache in front of the LLM gateway.
	// Absent / driver:"" => "none" (no caching; behaviour unchanged).
	Cache CacheConfig `yaml:"cache,omitempty"`
}

// CacheConfig configures the semantic-cache layer (Phase 15.5). The driver is
// resolved through the §4.4 cache seam (internal/llm/cache). Options is the
// driver-specific block (e.g. redis addr/password/db, weaviate endpoint).
type CacheConfig struct {
	// Driver: none|inmem|redis|weaviate|qdrant. Empty => none.
	Driver string `yaml:"driver,omitempty"`
	// Scope partitions cache keys within a tenant: tenant|customer|team|vk.
	// Empty => tenant (shared across the whole tenant). Cross-tenant sharing is
	// never possible (tenant_id always leads the key).
	Scope string `yaml:"scope,omitempty"`
	// TTL is the default entry lifetime (e.g. "5m"). Empty => driver default.
	TTL string `yaml:"ttl,omitempty"`
	// Threshold is the semantic-similarity floor (0–1) for semantic drivers.
	Threshold float32 `yaml:"threshold,omitempty"`
	// Options is the driver-specific config block.
	Options map[string]any `yaml:"options,omitempty"`
}

// AgentProfileConfig declares one Agent Profile (Phase 14) for cold-start
// seeding. The profile is the single source of truth for consumer entitlement;
// see docs/concepts/agent-profiles.md. Seeding is idempotent: a profile is
// matched to an existing row by (tenant, name) and updated in place, so a
// restart never duplicates it.
type AgentProfileConfig struct {
	// Tenant is the owning tenant id. Optional when exactly one tenant is
	// configured (it defaults to that tenant); required otherwise.
	Tenant              string   `yaml:"tenant,omitempty"`
	Name                string   `yaml:"name"`
	Description         string   `yaml:"description,omitempty"`
	AllowedMCPServers   []string `yaml:"allowed_mcp_servers,omitempty"`
	AllowedTools        []string `yaml:"allowed_tools,omitempty"`
	AllowedSkills       []string `yaml:"allowed_skills,omitempty"`
	AllowedModelAliases []string `yaml:"allowed_model_aliases,omitempty"`
	Scopes              []string `yaml:"scopes,omitempty"`
	// Bindings are JWT subjects bound to this profile at boot (idempotent).
	Bindings []string `yaml:"bindings,omitempty"`
	// Enabled defaults to true when omitted.
	Enabled *bool `yaml:"enabled,omitempty"`
}

// CodeModeConfig is the operator-tunable Code Mode policy (Phase 13.5). It gates
// the executeToolCode meta-tool itself; the per-tool-call governance of
// in-sandbox calls is unchanged. See docs/security/code-mode-threat-model.md.
type CodeModeConfig struct {
	// Disabled turns Code Mode off entirely (a kill switch): every
	// executeToolCode is denied regardless of the session opt-in.
	Disabled bool `yaml:"disabled,omitempty"`
	// MaxExecutionBytes rejects a snippet larger than this many bytes (0 = no limit).
	MaxExecutionBytes int `yaml:"max_execution_bytes,omitempty"`
	// MaxToolCallsInside caps tool calls per execution (0 = runtime default). A
	// ceiling: it lowers a larger session request, never raises a smaller one.
	MaxToolCallsInside int `yaml:"max_tool_calls_inside,omitempty"`
	// AllowedBindingLevels restricts which binding levels may run code mode
	// ("server"|"tool"). Empty = any.
	AllowedBindingLevels []string `yaml:"allowed_binding_levels,omitempty"`
	// RequireApprovalOnExecute gates every executeToolCode behind the approval
	// flow before the snippet runs.
	RequireApprovalOnExecute bool `yaml:"require_approval_on_execute,omitempty"`
	// DenyUnsafeStarlark escalates a static-gate rejection to an audited policy
	// denial. The static gate already rejects unsafe snippets; this records the
	// rejection as a policy event for operators tracking abuse.
	DenyUnsafeStarlark bool `yaml:"deny_unsafe_starlark,omitempty"`
}

// TelemetryConfig wires the OpenTelemetry tracer + drift detector knobs.
type TelemetryConfig struct {
	Enabled       bool              `yaml:"enabled,omitempty"`
	ServiceName   string            `yaml:"service_name,omitempty"`
	Exporter      string            `yaml:"exporter,omitempty"` // otlp_grpc | otlp_http | stdout | none
	OTLPEndpoint  string            `yaml:"otlp_endpoint,omitempty"`
	OTLPHeaders   map[string]string `yaml:"otlp_headers,omitempty"`
	SampleRate    float64           `yaml:"sample_rate,omitempty"`
	ResourceAttrs map[string]string `yaml:"resource_attrs,omitempty"`
	DriftInterval HumanDuration     `yaml:"drift_interval,omitempty"`
}

// HumanDuration accepts strings like "60s" / "5m" in YAML and falls back
// to the supplied default in Go. Lives here rather than being a
// time.Duration directly so YAML stays writeable without quoting.
type HumanDuration time.Duration

// Duration returns the underlying time.Duration, defaulting to 60s when
// unset.
func (h HumanDuration) Duration() time.Duration {
	if h == 0 {
		return 60 * time.Second
	}
	return time.Duration(h)
}

// UnmarshalYAML accepts both string ("60s") and integer (seconds) forms.
func (h *HumanDuration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err == nil && s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		*h = HumanDuration(d)
		return nil
	}
	var n int64
	if err := unmarshal(&n); err != nil {
		return err
	}
	*h = HumanDuration(time.Duration(n) * time.Second)
	return nil
}

// ServerConfig governs the HTTP listener.
type ServerConfig struct {
	Bind          string        `yaml:"bind"`           // e.g. "127.0.0.1:8080"
	ShutdownGrace time.Duration `yaml:"shutdown_grace"` // default 10s

	// AllowedOrigins is the allow-list applied to the Streamable HTTP
	// Origin guard (MCP spec 2025-11-25 requires 403 on invalid Origin).
	// Empty by default — programmatic clients without an Origin header
	// are always permitted; browser clients must be explicitly allowed.
	// Wildcard "*" allows any. Dev mode auto-permits localhost.
	AllowedOrigins []string `yaml:"allowed_origins,omitempty"`
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

// SkillsConfig groups skill-source declarations + runtime knobs.
type SkillsConfig struct {
	Sources []SkillSourceConfig `yaml:"sources"`
	// EnablementDefault is "opt-in" (must call enable to use) or
	// "auto" (every entitled skill enabled by default). Empty defaults
	// to opt-in.
	EnablementDefault string `yaml:"enablement_default,omitempty"`
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
// per-runtime-mode lifecycle, hot reload, and dynamic CRUD on top. Phase 5
// adds the optional Auth block.
type ServerSpec struct {
	ID          string     `yaml:"id"`
	DisplayName string     `yaml:"display_name,omitempty"`
	Transport   string     `yaml:"transport"` // stdio | http
	RuntimeMode string     `yaml:"runtime_mode,omitempty"`
	Stdio       *StdioSpec `yaml:"stdio,omitempty"`
	HTTP        *HTTPSpec  `yaml:"http,omitempty"`
	Auth        *AuthSpec  `yaml:"auth,omitempty"`
	// StartTimeout is the southbound-handshake budget (initialize round-trip).
	StartTimeout time.Duration `yaml:"start_timeout,omitempty"`
}

// AuthSpec is the YAML shape for per-server credential strategy +
// default risk class. Translated into registry.AuthSpec at boot.
type AuthSpec struct {
	Strategy         string             `yaml:"strategy,omitempty"`
	DefaultRiskClass string             `yaml:"default_risk_class,omitempty"`
	Env              []string           `yaml:"env,omitempty"`
	Headers          map[string]string  `yaml:"headers,omitempty"`
	SecretRef        string             `yaml:"secret_ref,omitempty"`
	Exchange         *OAuthExchangeSpec `yaml:"exchange,omitempty"`
}

// OAuthExchangeSpec is the YAML shape for RFC 8693 token exchange.
type OAuthExchangeSpec struct {
	TokenURL        string `yaml:"token_url"`
	ClientID        string `yaml:"client_id"`
	ClientSecretRef string `yaml:"client_secret_ref,omitempty"`
	Audience        string `yaml:"audience,omitempty"`
	Scope           string `yaml:"scope,omitempty"`
	GrantType       string `yaml:"grant_type,omitempty"`
	SubjectTokenSrc string `yaml:"subject_token_src,omitempty"`
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
