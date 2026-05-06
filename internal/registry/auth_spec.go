package registry

// AuthSpec configures credential resolution + default risk for a single
// downstream server. Phase 5 reads this; earlier phases parse and ignore.
type AuthSpec struct {
	// Strategy is the credential injector to apply. One of:
	// env_inject, http_header_inject, secret_reference, oauth2_token_exchange,
	// credential_shim. Empty means "no injection" (the supervisor still
	// honours static AuthHeader on HTTPSpec for back-compat).
	Strategy string `json:"strategy,omitempty" yaml:"strategy,omitempty"`

	// DefaultRiskClass is the per-server fallback applied when neither a
	// skill nor the engine config supplies an override. One of:
	// read, write, sensitive_read, external_side_effect, destructive.
	DefaultRiskClass string `json:"default_risk_class,omitempty" yaml:"default_risk_class,omitempty"`

	// Env declares the env list (KEY={{secret:name}} pairs) the
	// env_inject strategy applies. Mirrors StdioSpec.Env semantically but
	// keeps auth declarations co-located with the rest of the auth block.
	Env []string `json:"env,omitempty" yaml:"env,omitempty"`

	// Headers declares the static header set the http_header_inject
	// strategy emits. Keys are header names; values may carry
	// {{secret:name}} placeholders.
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`

	// SecretRef carries a single literal vault lookup (the
	// secret_reference strategy). Resolved value lands on the AuthHeader
	// of HTTPSpec or as `Authorization: Bearer <value>` when neither
	// strategy nor headers give an explicit shape.
	SecretRef string `json:"secret_ref,omitempty" yaml:"secret_ref,omitempty"`

	// Exchange holds the OAuth 2.0 token-exchange configuration when
	// Strategy = oauth2_token_exchange.
	Exchange *OAuthExchangeSpec `json:"exchange,omitempty" yaml:"exchange,omitempty"`
}

// OAuthExchangeSpec maps onto internal/secrets/oauth.ExchangeConfig.
type OAuthExchangeSpec struct {
	TokenURL        string `json:"token_url" yaml:"token_url"`
	ClientID        string `json:"client_id" yaml:"client_id"`
	ClientSecretRef string `json:"client_secret_ref,omitempty" yaml:"client_secret_ref,omitempty"`
	Audience        string `json:"audience,omitempty" yaml:"audience,omitempty"`
	Scope           string `json:"scope,omitempty" yaml:"scope,omitempty"`
	GrantType       string `json:"grant_type,omitempty" yaml:"grant_type,omitempty"`
	SubjectTokenSrc string `json:"subject_token_src,omitempty" yaml:"subject_token_src,omitempty"`
}
