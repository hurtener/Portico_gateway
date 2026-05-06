package inject_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/secrets/inject"
)

// fakeVault implements secrets.Vault with an in-memory map. Lives here to
// keep test deps minimal — the file vault test suite covers the real
// implementation.
type fakeVault struct {
	m map[string]string
}

func (f *fakeVault) Get(_ context.Context, tenantID, name string) (string, error) {
	v, ok := f.m[tenantID+"/"+name]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}
func (f *fakeVault) Put(_ context.Context, tenantID, name, value string) error {
	if f.m == nil {
		f.m = map[string]string{}
	}
	f.m[tenantID+"/"+name] = value
	return nil
}
func (f *fakeVault) Delete(_ context.Context, _, _ string) error    { return nil }
func (f *fakeVault) List(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (f *fakeVault) Close() error                                       { return nil }
func (f *fakeVault) RotateKey(_ context.Context, _ []byte) error        { return nil }

func TestEnvInject_VaultLookup(t *testing.T) {
	v := &fakeVault{m: map[string]string{"acme/pg_dsn": "postgres://..."}}
	in := inject.NewEnvInjector(v)
	target := &inject.PrepTarget{}
	err := in.Apply(context.Background(), inject.PrepRequest{
		TenantID: "acme",
		ServerSpec: &registry.ServerSpec{
			Auth: &registry.AuthSpec{Env: []string{"PG_DSN={{secret:pg_dsn}}"}},
		},
	}, target)
	if err != nil {
		t.Fatal(err)
	}
	if target.Env["PG_DSN"] != "postgres://..." {
		t.Errorf("env = %v", target.Env)
	}
}

func TestEnvInject_LiteralPassthrough(t *testing.T) {
	v := &fakeVault{}
	in := inject.NewEnvInjector(v)
	target := &inject.PrepTarget{}
	err := in.Apply(context.Background(), inject.PrepRequest{
		TenantID: "acme",
		ServerSpec: &registry.ServerSpec{
			Auth: &registry.AuthSpec{Env: []string{"DEBUG=1"}},
		},
	}, target)
	if err != nil {
		t.Fatal(err)
	}
	if target.Env["DEBUG"] != "1" {
		t.Errorf("env = %v", target.Env)
	}
}

func TestEnvInject_MissingSecret(t *testing.T) {
	v := &fakeVault{}
	in := inject.NewEnvInjector(v)
	target := &inject.PrepTarget{}
	err := in.Apply(context.Background(), inject.PrepRequest{
		TenantID: "acme",
		ServerSpec: &registry.ServerSpec{
			Auth: &registry.AuthSpec{Env: []string{"X={{secret:missing}}"}},
		},
	}, target)
	if err == nil {
		t.Errorf("expected error for missing secret")
	}
}

func TestHTTPHeaderInject_VaultLookup(t *testing.T) {
	v := &fakeVault{m: map[string]string{"acme/api_key": "key-123"}}
	in := inject.NewHTTPHeaderInjector(v)
	target := &inject.PrepTarget{}
	err := in.Apply(context.Background(), inject.PrepRequest{
		TenantID: "acme",
		ServerSpec: &registry.ServerSpec{
			Auth: &registry.AuthSpec{Headers: map[string]string{"X-API-Key": "{{secret:api_key}}"}},
		},
	}, target)
	if err != nil {
		t.Fatal(err)
	}
	if target.Headers["X-API-Key"] != "key-123" {
		t.Errorf("headers = %v", target.Headers)
	}
}

func TestSecretRef_BearerInjection(t *testing.T) {
	v := &fakeVault{m: map[string]string{"acme/gh_token": "ghp_abc"}}
	in := inject.NewSecretRefInjector(v)
	target := &inject.PrepTarget{}
	err := in.Apply(context.Background(), inject.PrepRequest{
		TenantID: "acme",
		ServerSpec: &registry.ServerSpec{
			Auth: &registry.AuthSpec{SecretRef: "gh_token"},
		},
	}, target)
	if err != nil {
		t.Fatal(err)
	}
	if got := target.Headers["Authorization"]; got != "Bearer ghp_abc" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestShim_NotImplemented(t *testing.T) {
	in := inject.NewShimInjector()
	err := in.Apply(context.Background(), inject.PrepRequest{}, &inject.PrepTarget{})
	if !errors.Is(err, inject.ErrNotImplemented) {
		t.Errorf("expected ErrNotImplemented, got %v", err)
	}
}

func TestRegistry_PanicsOnDuplicate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on duplicate registration")
		}
	}()
	r := inject.NewRegistry()
	r.Register(inject.NewShimInjector())
	r.Register(inject.NewShimInjector())
}

func TestRegistry_Get(t *testing.T) {
	r := inject.NewRegistry()
	r.Register(inject.NewShimInjector())
	if _, ok := r.Get(inject.StrategyCredentialShim); !ok {
		t.Errorf("expected strategy registered")
	}
	if _, ok := r.Get("nonexistent"); ok {
		t.Errorf("expected miss for unknown strategy")
	}
}
