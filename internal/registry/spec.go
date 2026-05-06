// Package registry owns the per-tenant catalog of MCP server specs and the
// run-time bookkeeping for their instances. Phase 2 ships:
//
//   - ServerSpec: the canonical definition of a registered MCP server,
//     covering transport, runtime mode, lifecycle, health, limits, env.
//   - Validate: enforces the schema and applies sane defaults.
//   - ResolveEffective: merges a global spec with a per-tenant override.
//
// Phase 3+ will extend the spec with policy, namespace allowlists, and
// resource/prompt routing knobs.
package registry

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// V1 runtime modes. shared_global runs one process for all tenants/users;
// per_tenant gives each tenant its own; per_user further partitions by
// JWT subject; per_session gives each MCP session its own (most isolated);
// remote_static is for HTTP downstreams that do not have a managed process.
const (
	ModeSharedGlobal = "shared_global"
	ModePerTenant    = "per_tenant"
	ModePerUser      = "per_user"
	ModePerSession   = "per_session"
	ModeRemoteStatic = "remote_static"
)

// Status values surfaced via the API. The supervisor transitions a server
// through these as instances start, fail, or recover.
const (
	StatusUnknown      = "unknown"
	StatusStarting     = "starting"
	StatusHealthy      = "healthy"
	StatusUnhealthy    = "unhealthy"
	StatusDisabled     = "disabled"
	StatusCircuitOpen  = "circuit_open"
)

// ServerSpec is the canonical representation of a registered MCP server.
// Maps onto YAML config and the JSON body of /v1/servers.
type ServerSpec struct {
	ID          string         `json:"id" yaml:"id"`
	DisplayName string         `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Transport   string         `json:"transport" yaml:"transport"`
	RuntimeMode string         `json:"runtime_mode,omitempty" yaml:"runtime_mode,omitempty"`
	Stdio       *StdioSpec     `json:"stdio,omitempty" yaml:"stdio,omitempty"`
	HTTP        *HTTPSpec      `json:"http,omitempty" yaml:"http,omitempty"`
	Health      HealthSpec     `json:"health,omitempty" yaml:"health,omitempty"`
	Lifecycle   LifecycleSpec  `json:"lifecycle,omitempty" yaml:"lifecycle,omitempty"`
	Limits      ResourceLimits `json:"limits,omitempty" yaml:"limits,omitempty"`
	Enabled     *bool          `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

type StdioSpec struct {
	Command      string   `json:"command" yaml:"command"`
	Args         []string `json:"args,omitempty" yaml:"args,omitempty"`
	Env          []string `json:"env,omitempty" yaml:"env,omitempty"`
	Cwd          string   `json:"cwd,omitempty" yaml:"cwd,omitempty"`
	StartTimeout Duration `json:"start_timeout,omitempty" yaml:"start_timeout,omitempty"`
}

type HTTPSpec struct {
	URL        string   `json:"url" yaml:"url"`
	AuthHeader string   `json:"auth_header,omitempty" yaml:"auth_header,omitempty"`
	Timeout    Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

type HealthSpec struct {
	PingInterval Duration `json:"ping_interval,omitempty" yaml:"ping_interval,omitempty"`
	PingTimeout  Duration `json:"ping_timeout,omitempty" yaml:"ping_timeout,omitempty"`
	StartupGrace Duration `json:"startup_grace,omitempty" yaml:"startup_grace,omitempty"`
}

type LifecycleSpec struct {
	IdleTimeout         Duration `json:"idle_timeout,omitempty" yaml:"idle_timeout,omitempty"`
	BackoffInitial      Duration `json:"backoff_initial,omitempty" yaml:"backoff_initial,omitempty"`
	BackoffMax          Duration `json:"backoff_max,omitempty" yaml:"backoff_max,omitempty"`
	MaxRestartAttempts  int      `json:"max_restart_attempts,omitempty" yaml:"max_restart_attempts,omitempty"`
	CircuitOpenDuration Duration `json:"circuit_open_duration,omitempty" yaml:"circuit_open_duration,omitempty"`
	ShutdownGrace       Duration `json:"shutdown_grace,omitempty" yaml:"shutdown_grace,omitempty"`
}

type ResourceLimits struct {
	MemoryMax     string `json:"memory_max,omitempty" yaml:"memory_max,omitempty"`
	CPUMillicores int    `json:"cpu_millicores,omitempty" yaml:"cpu_millicores,omitempty"`
	OpenFiles     int    `json:"open_files,omitempty" yaml:"open_files,omitempty"`
	Processes     int    `json:"processes,omitempty" yaml:"processes,omitempty"`
}

// Duration wraps time.Duration with YAML/JSON marshalling that accepts
// human strings ("5s", "1m") and emits the same.
type Duration time.Duration

// UnmarshalYAML accepts strings ("5s") or numbers (seconds, for compat).
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!str" || node.Kind == yaml.ScalarNode && node.Style != 0 {
		td, err := time.ParseDuration(node.Value)
		if err != nil {
			return fmt.Errorf("duration: %w", err)
		}
		*d = Duration(td)
		return nil
	}
	// Try string first (covers !!str and unquoted "5s")
	var s string
	if err := node.Decode(&s); err == nil && s != "" {
		td, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("duration: %w", err)
		}
		*d = Duration(td)
		return nil
	}
	var i int64
	if err := node.Decode(&i); err == nil {
		*d = Duration(time.Duration(i) * time.Second)
		return nil
	}
	return fmt.Errorf("duration: must be a string like '5s' or an integer (seconds)")
}

// UnmarshalJSON accepts strings ("5s") or numbers (nanoseconds, like time.Duration).
func (d *Duration) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		td, err := time.ParseDuration(s[1 : len(s)-1])
		if err != nil {
			return fmt.Errorf("duration: %w", err)
		}
		*d = Duration(td)
		return nil
	}
	// Numeric JSON: treat as seconds for ergonomic config.
	var n int64
	if _, err := fmt.Sscanf(s, "%d", &n); err == nil {
		*d = Duration(time.Duration(n) * time.Second)
		return nil
	}
	return fmt.Errorf("duration: invalid value %q", s)
}

// MarshalJSON emits the human form ("5s").
func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Duration(d).String() + `"`), nil
}

// Std returns the underlying time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// idRegexp mirrors the namespace package's server-id constraints. Kept in
// sync by hand; namespace.ValidateServerID is the runtime check.
var idRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// ValidModes enumerates the runtime modes accepted by V1.
var ValidModes = map[string]struct{}{
	ModeSharedGlobal: {},
	ModePerTenant:    {},
	ModePerUser:      {},
	ModePerSession:   {},
	ModeRemoteStatic: {},
}

// Validate enforces the spec invariants and applies defaults. Returns a
// FieldError naming the offending field when something is wrong.
func (s *ServerSpec) Validate() error {
	if s == nil {
		return fieldErr("spec", "is required")
	}
	if s.ID == "" {
		return fieldErr("id", "is required")
	}
	if !idRegexp.MatchString(s.ID) {
		return fieldErr("id", fmt.Sprintf("invalid format %q (must match %s)", s.ID, idRegexp.String()))
	}
	if s.DisplayName == "" {
		s.DisplayName = s.ID
	}
	if s.Transport == "" {
		return fieldErr("transport", "is required")
	}
	if s.Transport != "stdio" && s.Transport != "http" {
		return fieldErr("transport", fmt.Sprintf("unsupported %q (want stdio or http)", s.Transport))
	}
	if s.RuntimeMode == "" {
		if s.Transport == "http" {
			s.RuntimeMode = ModeRemoteStatic
		} else {
			s.RuntimeMode = ModeSharedGlobal
		}
	}
	if _, ok := ValidModes[s.RuntimeMode]; !ok {
		return fieldErr("runtime_mode", fmt.Sprintf("invalid %q", s.RuntimeMode))
	}
	if s.Transport == "stdio" && s.RuntimeMode == ModeRemoteStatic {
		return fieldErr("runtime_mode", "remote_static is only valid for transport=http")
	}
	if s.Transport == "http" && s.RuntimeMode != ModeRemoteStatic {
		return fieldErr("runtime_mode", "http transport requires runtime_mode=remote_static")
	}

	switch s.Transport {
	case "stdio":
		if s.Stdio == nil || s.Stdio.Command == "" {
			return fieldErr("stdio.command", "is required when transport=stdio")
		}
		if s.Stdio.StartTimeout == 0 {
			s.Stdio.StartTimeout = Duration(10 * time.Second)
		}
	case "http":
		if s.HTTP == nil || s.HTTP.URL == "" {
			return fieldErr("http.url", "is required when transport=http")
		}
		if _, err := url.Parse(s.HTTP.URL); err != nil {
			return fieldErr("http.url", fmt.Sprintf("invalid url %q: %v", s.HTTP.URL, err))
		}
		if s.HTTP.Timeout == 0 {
			s.HTTP.Timeout = Duration(30 * time.Second)
		}
	}

	// Defaults
	if s.Health.PingTimeout == 0 {
		s.Health.PingTimeout = Duration(5 * time.Second)
	}
	if s.Health.StartupGrace == 0 {
		s.Health.StartupGrace = Duration(5 * time.Second)
	}
	// PingInterval==0 disables the periodic probe; leave as-is.
	if s.Lifecycle.BackoffInitial == 0 {
		s.Lifecycle.BackoffInitial = Duration(500 * time.Millisecond)
	}
	if s.Lifecycle.BackoffMax == 0 {
		s.Lifecycle.BackoffMax = Duration(30 * time.Second)
	}
	if s.Lifecycle.MaxRestartAttempts == 0 {
		s.Lifecycle.MaxRestartAttempts = 5
	}
	if s.Lifecycle.CircuitOpenDuration == 0 {
		s.Lifecycle.CircuitOpenDuration = Duration(5 * time.Minute)
	}
	if s.Lifecycle.ShutdownGrace == 0 {
		s.Lifecycle.ShutdownGrace = Duration(5 * time.Second)
	}
	return nil
}

// FieldError mirrors the config package's error shape so API handlers can
// surface a structured 400.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

func fieldErr(field, msg string) error { return &FieldError{Field: field, Message: msg} }
