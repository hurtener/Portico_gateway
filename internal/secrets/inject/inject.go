// Package inject owns the credential-strategy seam between Portico's auth
// configuration and the southbound transports. Every supported strategy
// (env_inject, http_header_inject, secret_reference, oauth2_token_exchange,
// credential_shim) implements Injector and writes to a PrepTarget the
// runtime hands to the supervisor / southbound HTTP RoundTripper.
//
// V1 ships one strategy per server: spec.Auth.Strategy selects the
// Injector. The interface is built for an eventual list (multi-strategy
// composition) so future credential pipelines (e.g. exchange + custom
// header) don't break the seam.
package inject

import (
	"context"
	"errors"

	"github.com/hurtener/Portico_gateway/internal/registry"
)

// Strategy names. Configuration files reference these constants via the
// AuthSpec.Strategy field; the registry validates the value at parse time.
const (
	StrategyEnvInject       = "env_inject"
	StrategyHTTPHeader      = "http_header_inject"
	StrategySecretReference = "secret_reference"
	StrategyOAuth2Exchange  = "oauth2_token_exchange"
	StrategyCredentialShim  = "credential_shim"
)

// ErrNotImplemented is returned by injectors whose strategy is reserved
// for a future phase (e.g. credential_shim). Callers may convert to a
// structured policy error so operators get a clear message.
var ErrNotImplemented = errors.New("inject: strategy not implemented in V1")

// PrepRequest is the dispatcher's view of the call: identity, the raw
// subject token (the incoming JWT) when available for OAuth exchange, and
// the resolved server spec.
type PrepRequest struct {
	TenantID     string
	UserID       string
	SessionID    string
	SubjectToken string // raw incoming JWT; empty in dev mode
	ServerSpec   *registry.ServerSpec
}

// PrepTarget is the mutable surface injectors write to. Env populates the
// child process environment for stdio servers; Headers populates outbound
// HTTP requests for remote_static servers.
//
// Both maps are non-nil when handed to an injector; the runtime constructs
// fresh maps per call so a previous tenant's secrets cannot leak into the
// current request.
type PrepTarget struct {
	Env     map[string]string
	Headers map[string]string
}

// Injector is the contract every credential strategy implements.
type Injector interface {
	// Strategy returns the constant this injector handles. Lets the
	// registry dispatch generically.
	Strategy() string

	// Apply mutates target with the credentials this strategy resolves.
	// Errors surface as -32003 policy_denied with reason credential_lookup_failed
	// when the dispatcher catches them; ErrNotImplemented becomes
	// reason: strategy_not_supported.
	Apply(ctx context.Context, req PrepRequest, target *PrepTarget) error
}

// Registry maps strategy name → Injector. The runtime constructs one at
// startup with the active vault and OAuth exchanger; tests can inject
// fakes by registering bespoke strategies.
type Registry struct {
	byName map[string]Injector
}

// NewRegistry returns an empty Registry. Production callers immediately
// register the V1 injector set.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Injector)}
}

// Register adds an Injector. Re-registering the same strategy panics —
// this is a programming error caught at startup.
func (r *Registry) Register(in Injector) {
	if in == nil {
		return
	}
	if _, exists := r.byName[in.Strategy()]; exists {
		panic("inject: strategy registered twice: " + in.Strategy())
	}
	r.byName[in.Strategy()] = in
}

// Get returns the Injector for strategy, or ok=false when no implementation
// is registered. Strategies absent from the registry surface as
// ErrNotImplemented.
func (r *Registry) Get(strategy string) (Injector, bool) {
	if r == nil {
		return nil, false
	}
	in, ok := r.byName[strategy]
	return in, ok
}
