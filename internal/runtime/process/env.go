package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/hurtener/Portico_gateway/internal/secrets"
)

// Resolver expands the {{secret:...}} and {{env:...}} placeholders inside
// an env list. Tenant-scoped: secret lookups always go through the vault
// with the supplied tenantID.
type Resolver struct {
	vault secrets.Vault
}

// NewResolver constructs a Resolver. A nil vault is allowed: secret
// references then surface as ErrSecretLookup so callers fail server start
// with a clear message.
func NewResolver(v secrets.Vault) *Resolver { return &Resolver{vault: v} }

// ErrSecretLookup is returned when a {{secret:...}} reference cannot be
// resolved. Wrapped with the placeholder name so the operator can debug.
var ErrSecretLookup = errors.New("secret lookup failed")

// placeholderRE matches {{kind:value}}. Kind is "secret" or "env".
var placeholderRE = regexp.MustCompile(`\{\{\s*(secret|env)\s*:\s*([A-Za-z0-9_.-]+)\s*\}\}`)

// Resolve walks each "KEY=VALUE" entry, replaces every placeholder in
// VALUE, and returns the new slice. Returns an error on the first
// unresolvable reference. Unknown placeholder shapes are an error so a
// typo doesn't silently leak the literal string into the child env.
func (r *Resolver) Resolve(ctx context.Context, tenantID string, env []string) ([]string, error) {
	out := make([]string, len(env))
	for i, kv := range env {
		eq := indexEq(kv)
		if eq < 0 {
			return nil, fmt.Errorf("env: %q: missing '=' separator", kv)
		}
		key := kv[:eq]
		val := kv[eq+1:]
		resolved, err := r.resolveOne(ctx, tenantID, val)
		if err != nil {
			return nil, fmt.Errorf("env %s: %w", key, err)
		}
		out[i] = key + "=" + resolved
	}
	return out, nil
}

func (r *Resolver) resolveOne(ctx context.Context, tenantID, in string) (string, error) {
	var firstErr error
	out := placeholderRE.ReplaceAllStringFunc(in, func(match string) string {
		if firstErr != nil {
			return match
		}
		groups := placeholderRE.FindStringSubmatch(match)
		kind, name := groups[1], groups[2]
		switch kind {
		case "secret":
			if r.vault == nil {
				firstErr = fmt.Errorf("%w: %s (no vault configured; set PORTICO_VAULT_KEY)", ErrSecretLookup, name)
				return match
			}
			val, err := r.vault.Get(ctx, tenantID, name)
			if err != nil {
				firstErr = fmt.Errorf("%w: %s: %v", ErrSecretLookup, name, err)
				return match
			}
			return val
		case "env":
			val, ok := os.LookupEnv(name)
			if !ok {
				firstErr = fmt.Errorf("env: %s not set in process environment", name)
				return match
			}
			return val
		default:
			firstErr = fmt.Errorf("env: unknown placeholder kind %q", kind)
			return match
		}
	})
	if firstErr != nil {
		return "", firstErr
	}
	// Detect malformed `{{...}}` left in the value (looks like a placeholder
	// but did not match our pattern: typo or stray braces).
	if hasUnknownPlaceholder(out) {
		return "", fmt.Errorf("env: unrecognised placeholder in %q", in)
	}
	return out, nil
}

func indexEq(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return i
		}
	}
	return -1
}

func hasUnknownPlaceholder(s string) bool {
	// We've already substituted known patterns; if any "{{" remains,
	// it's a typo or unsupported shape.
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '{' && s[i+1] == '{' {
			return true
		}
	}
	return false
}
