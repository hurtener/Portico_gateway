package process_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/runtime/process"
	"github.com/hurtener/Portico_gateway/internal/secrets"
)

type fakeVault struct {
	entries map[string]map[string]string
}

func (v *fakeVault) Get(_ context.Context, tenantID, name string) (string, error) {
	if v.entries[tenantID] == nil {
		return "", secrets.ErrNotFound
	}
	val, ok := v.entries[tenantID][name]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return val, nil
}
func (v *fakeVault) Put(_ context.Context, tenantID, name, value string) error {
	if v.entries[tenantID] == nil {
		v.entries[tenantID] = map[string]string{}
	}
	v.entries[tenantID][name] = value
	return nil
}
func (v *fakeVault) Delete(_ context.Context, tenantID, name string) error { return nil }
func (v *fakeVault) List(_ context.Context, tenantID string) ([]string, error) {
	out := make([]string, 0, len(v.entries[tenantID]))
	for k := range v.entries[tenantID] {
		out = append(out, k)
	}
	return out, nil
}
func (v *fakeVault) Close() error { return nil }

func TestResolver_SecretInterpolation(t *testing.T) {
	v := &fakeVault{entries: map[string]map[string]string{
		"acme": {"github_token": "ghp_xyz"},
	}}
	r := process.NewResolver(v)
	out, err := r.Resolve(context.Background(), "acme", []string{"GITHUB_TOKEN={{secret:github_token}}"})
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != "GITHUB_TOKEN=ghp_xyz" {
		t.Errorf("got %q", out[0])
	}
}

func TestResolver_TenantScoped(t *testing.T) {
	v := &fakeVault{entries: map[string]map[string]string{
		"acme": {"k": "A"},
		"beta": {"k": "B"},
	}}
	r := process.NewResolver(v)
	a, _ := r.Resolve(context.Background(), "acme", []string{"X={{secret:k}}"})
	b, _ := r.Resolve(context.Background(), "beta", []string{"X={{secret:k}}"})
	if a[0] != "X=A" || b[0] != "X=B" {
		t.Errorf("acme=%v beta=%v", a, b)
	}
}

func TestResolver_MissingSecretFails(t *testing.T) {
	v := &fakeVault{entries: map[string]map[string]string{"acme": {}}}
	r := process.NewResolver(v)
	_, err := r.Resolve(context.Background(), "acme", []string{"X={{secret:missing}}"})
	if err == nil {
		t.Fatal("expected error on missing secret")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("err should mention placeholder name: %v", err)
	}
}

func TestResolver_NoVaultRejectsSecretReference(t *testing.T) {
	r := process.NewResolver(nil)
	_, err := r.Resolve(context.Background(), "acme", []string{"X={{secret:k}}"})
	if err == nil {
		t.Fatal("expected error: secret reference without vault")
	}
}

func TestResolver_EnvPassthrough(t *testing.T) {
	t.Setenv("PORTICO_TEST_VAR", "from-os")
	r := process.NewResolver(nil)
	out, err := r.Resolve(context.Background(), "acme", []string{"X={{env:PORTICO_TEST_VAR}}"})
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != "X=from-os" {
		t.Errorf("got %q", out[0])
	}
}

func TestResolver_UnknownPlaceholderFails(t *testing.T) {
	r := process.NewResolver(nil)
	if _, err := r.Resolve(context.Background(), "acme", []string{"X={{unknown:val}}"}); err == nil {
		t.Fatal("expected error: unrecognised placeholder")
	}
}

func TestResolver_LeavesPlainValuesAlone(t *testing.T) {
	r := process.NewResolver(nil)
	out, err := r.Resolve(context.Background(), "acme", []string{"X=hello", "Y=world"})
	if err != nil {
		t.Fatal(err)
	}
	if out[0] != "X=hello" || out[1] != "Y=world" {
		t.Errorf("got %v", out)
	}
}

// suppress unused import warning when env not consulted.
var _ = os.Getenv
