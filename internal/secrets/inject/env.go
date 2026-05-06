package inject

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/hurtener/Portico_gateway/internal/secrets"
)

// envInjector resolves `KEY={{secret:name}}` pairs declared on the
// server's auth.env list. Used for stdio servers where credentials live
// in the child environment.
type envInjector struct {
	vault secrets.Vault
}

// NewEnvInjector builds an env_inject strategy backed by vault.
func NewEnvInjector(v secrets.Vault) Injector { return &envInjector{vault: v} }

func (e *envInjector) Strategy() string { return StrategyEnvInject }

func (e *envInjector) Apply(ctx context.Context, req PrepRequest, target *PrepTarget) error {
	if req.ServerSpec == nil || req.ServerSpec.Auth == nil {
		return nil
	}
	pairs := req.ServerSpec.Auth.Env
	if len(pairs) == 0 {
		return nil
	}
	if target.Env == nil {
		target.Env = make(map[string]string, len(pairs))
	}
	for _, kv := range pairs {
		key, val, ok := splitKV(kv)
		if !ok {
			return fmt.Errorf("inject env: %q: missing '=' separator", kv)
		}
		resolved, err := resolveSecretRefs(ctx, e.vault, req.TenantID, val)
		if err != nil {
			return fmt.Errorf("inject env %s: %w", key, err)
		}
		target.Env[key] = resolved
	}
	return nil
}

// httpHeaderInjector resolves `Header: {{secret:name}}` entries declared
// on the server's auth.headers map. Used for HTTP southbound clients.
type httpHeaderInjector struct {
	vault secrets.Vault
}

// NewHTTPHeaderInjector builds an http_header_inject strategy.
func NewHTTPHeaderInjector(v secrets.Vault) Injector { return &httpHeaderInjector{vault: v} }

func (h *httpHeaderInjector) Strategy() string { return StrategyHTTPHeader }

func (h *httpHeaderInjector) Apply(ctx context.Context, req PrepRequest, target *PrepTarget) error {
	if req.ServerSpec == nil || req.ServerSpec.Auth == nil {
		return nil
	}
	headers := req.ServerSpec.Auth.Headers
	if len(headers) == 0 {
		return nil
	}
	if target.Headers == nil {
		target.Headers = make(map[string]string, len(headers))
	}
	for k, v := range headers {
		resolved, err := resolveSecretRefs(ctx, h.vault, req.TenantID, v)
		if err != nil {
			return fmt.Errorf("inject header %s: %w", k, err)
		}
		target.Headers[k] = resolved
	}
	return nil
}

// secretRefInjector resolves a single literal secret_ref from auth.secret_ref
// and writes it as `Authorization: Bearer <value>`. Used by servers that
// just need a single token plumbed through.
type secretRefInjector struct {
	vault secrets.Vault
}

// NewSecretRefInjector builds a secret_reference strategy.
func NewSecretRefInjector(v secrets.Vault) Injector { return &secretRefInjector{vault: v} }

func (s *secretRefInjector) Strategy() string { return StrategySecretReference }

func (s *secretRefInjector) Apply(ctx context.Context, req PrepRequest, target *PrepTarget) error {
	if req.ServerSpec == nil || req.ServerSpec.Auth == nil || req.ServerSpec.Auth.SecretRef == "" {
		return nil
	}
	if s.vault == nil {
		return errors.New("inject secret_reference: vault not configured")
	}
	val, err := s.vault.Get(ctx, req.TenantID, req.ServerSpec.Auth.SecretRef)
	if err != nil {
		return fmt.Errorf("inject secret_reference %s: %w", req.ServerSpec.Auth.SecretRef, err)
	}
	if target.Headers == nil {
		target.Headers = make(map[string]string, 1)
	}
	target.Headers["Authorization"] = "Bearer " + val
	return nil
}

// shimInjector is the V1 placeholder for credential_shim. Returns
// ErrNotImplemented so the dispatcher surfaces a clean policy error.
type shimInjector struct{}

// NewShimInjector returns a stub injector for credential_shim.
func NewShimInjector() Injector { return &shimInjector{} }

func (shimInjector) Strategy() string { return StrategyCredentialShim }

func (shimInjector) Apply(_ context.Context, _ PrepRequest, _ *PrepTarget) error {
	return ErrNotImplemented
}

// secretRefRE matches the {{secret:name}} placeholder used across stdio
// env, headers, and (future) URL templates. Keeps grammar consistent with
// internal/runtime/process/env.go's pattern.
var secretRefRE = regexp.MustCompile(`\{\{\s*secret\s*:\s*([A-Za-z0-9_.-]+)\s*\}\}`)

// resolveSecretRefs expands every {{secret:name}} placeholder in s using
// vault.Get(tenantID, name). A nil vault with placeholders present is an
// error (the operator wired a strategy that needs credentials but did not
// configure PORTICO_VAULT_KEY).
func resolveSecretRefs(ctx context.Context, vault secrets.Vault, tenantID, s string) (string, error) {
	matches := secretRefRE.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s, nil
	}
	if vault == nil {
		return "", errors.New("vault not configured")
	}
	var out []byte
	prev := 0
	for _, m := range matches {
		// m[0..1] is the full match; m[2..3] is the capture (name).
		out = append(out, s[prev:m[0]]...)
		name := s[m[2]:m[3]]
		val, err := vault.Get(ctx, tenantID, name)
		if err != nil {
			return "", fmt.Errorf("secret %s: %w", name, err)
		}
		out = append(out, val...)
		prev = m[1]
	}
	out = append(out, s[prev:]...)
	return string(out), nil
}

// splitKV splits "KEY=VALUE" preserving any '=' inside VALUE.
func splitKV(kv string) (string, string, bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return "", "", false
}
