package bifrost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/maximhq/bifrost/core/schemas"

	"github.com/hurtener/Portico_gateway/internal/llm/engine/ifaces"
	"github.com/hurtener/Portico_gateway/internal/secrets"
	storageifaces "github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// porticoAccount implements the Bifrost schemas.Account interface for a single
// tenant. Bifrost calls back into it on every dispatch to learn which providers
// are configured and to fetch their API keys — so Portico's per-tenant registry
// and vault remain the source of truth, never an in-memory snapshot.
//
// One porticoAccount (and one Bifrost client) exists per tenant: the Account
// interface is not tenant-parameterised, so tenant isolation is achieved by
// binding the account to a tenantID at construction.
//
// Bifrost keys providers by their driver-level ModelProvider ("openai",
// "anthropic", …). A Portico tenant may register several providers that share a
// driver (e.g. two "openai" entries with different keys); this account aggregates
// all of them under the single Bifrost provider for that driver.
type porticoAccount struct {
	deps     ifaces.Deps
	tenantID string
}

// GetConfiguredProviders returns the distinct enabled driver names for this tenant.
func (p *porticoAccount) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	provs, err := p.deps.Providers.ListProviders(context.Background(), p.tenantID)
	if err != nil {
		return nil, fmt.Errorf("bifrost account: list providers: %w", err)
	}
	seen := make(map[string]struct{})
	out := make([]schemas.ModelProvider, 0, len(provs))
	for _, pr := range provs {
		if !pr.Enabled {
			continue
		}
		if _, dup := seen[pr.Driver]; dup {
			continue
		}
		seen[pr.Driver] = struct{}{}
		out = append(out, schemas.ModelProvider(pr.Driver))
	}
	return out, nil
}

// GetKeysForProvider resolves API keys for every enabled provider with the given
// driver, dereferencing each credential against the tenant's vault on every call
// (no plaintext caching beyond this scope).
func (p *porticoAccount) GetKeysForProvider(ctx context.Context, providerKey schemas.ModelProvider) ([]schemas.Key, error) {
	provs, err := p.providersForDriver(ctx, string(providerKey))
	if err != nil {
		return nil, err
	}
	out := make([]schemas.Key, 0, len(provs))
	for _, pr := range provs {
		keyRows, err := p.deps.Providers.ListKeys(ctx, p.tenantID, pr.Name)
		if err != nil {
			return nil, fmt.Errorf("bifrost account: list keys: %w", err)
		}
		for _, k := range keyRows {
			if !k.Enabled {
				continue
			}
			secret, err := p.vaultGet(ctx, k.CredentialRef)
			if err != nil {
				return nil, err
			}
			if secret == "" {
				continue
			}
			out = append(out, schemas.Key{
				ID:     k.KeyID,
				Name:   k.KeyID,
				Value:  schemas.EnvVar{Val: secret},
				Models: modelAllowlist(k.ModelAllowlist),
				Weight: k.Weight,
			})
		}
		// Fall back to the provider's default credential_ref when it has no key rows.
		if len(keyRows) == 0 && pr.CredentialRef != "" {
			secret, err := p.vaultGet(ctx, pr.CredentialRef)
			if err != nil {
				return nil, err
			}
			if secret != "" {
				out = append(out, schemas.Key{
					ID:     pr.Name,
					Name:   pr.Name,
					Value:  schemas.EnvVar{Val: secret},
					Models: nil,
					Weight: 1.0,
				})
			}
		}
	}
	return out, nil
}

// GetConfigForProvider returns the network config for a driver. The first enabled
// provider of that driver supplies an optional base_url + headers (used by
// custom-OpenAI-compatible endpoints to ride Bifrost's OpenAI code path).
func (p *porticoAccount) GetConfigForProvider(providerKey schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	cfg := &schemas.ProviderConfig{}
	provs, err := p.providersForDriver(context.Background(), string(providerKey))
	if err != nil || len(provs) == 0 {
		cfg.CheckAndSetDefaults()
		return cfg, nil
	}
	var raw struct {
		BaseURL      string            `json:"base_url"`
		Endpoint     string            `json:"endpoint"`
		ExtraHeaders map[string]string `json:"headers"`
	}
	if provs[0].ConfigJSON != "" {
		_ = json.Unmarshal([]byte(provs[0].ConfigJSON), &raw)
	}
	if raw.BaseURL == "" {
		raw.BaseURL = raw.Endpoint
	}
	if raw.BaseURL != "" {
		cfg.NetworkConfig.BaseURL = raw.BaseURL
	}
	if len(raw.ExtraHeaders) > 0 {
		cfg.NetworkConfig.ExtraHeaders = raw.ExtraHeaders
	}
	cfg.CheckAndSetDefaults()
	return cfg, nil
}

// providersForDriver returns the tenant's enabled providers with the given driver.
func (p *porticoAccount) providersForDriver(ctx context.Context, driver string) ([]*storageifaces.LLMProvider, error) {
	provs, err := p.deps.Providers.ListProviders(ctx, p.tenantID)
	if err != nil {
		return nil, fmt.Errorf("bifrost account: list providers: %w", err)
	}
	out := make([]*storageifaces.LLMProvider, 0, len(provs))
	for _, pr := range provs {
		if pr.Enabled && pr.Driver == driver {
			out = append(out, pr)
		}
	}
	return out, nil
}

// vaultGet returns the secret for a credential ref, treating a missing entry as
// empty (a dangling ref is skipped, not fatal).
func (p *porticoAccount) vaultGet(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	secret, err := p.deps.Vault.Get(ctx, p.tenantID, ref)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("bifrost account: vault get %q: %w", ref, err)
	}
	return secret, nil
}

// modelAllowlist parses the JSON-array allowlist into Bifrost's []string form.
// An empty/absent allowlist means "all models" — which Bifrost expresses as an
// EMPTY slice (it matches a key to a model via len(Models)==0 || Contains(Models,
// model); "*" is a literal, NOT a wildcard).
func modelAllowlist(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err != nil || len(models) == 0 {
		return nil
	}
	return models
}
