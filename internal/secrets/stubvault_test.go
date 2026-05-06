package secrets_test

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/secrets"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestFileVault_PutGetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	v, err := secrets.NewFileVault(filepath.Join(dir, "vault.yaml"), newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := v.Put(ctx, "acme", "github_token", "ghp_xxx"); err != nil {
		t.Fatal(err)
	}
	got, err := v.Get(ctx, "acme", "github_token")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ghp_xxx" {
		t.Errorf("got %q want ghp_xxx", got)
	}
}

func TestFileVault_TenantScoping(t *testing.T) {
	dir := t.TempDir()
	v, err := secrets.NewFileVault(filepath.Join(dir, "vault.yaml"), newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := v.Put(ctx, "acme", "shared_name", "secret-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get(ctx, "beta", "shared_name"); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("cross-tenant read should return ErrNotFound, got %v", err)
	}
}

func TestFileVault_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.yaml")
	key := newKey(t)
	v1, err := secrets.NewFileVault(path, key)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := v1.Put(ctx, "acme", "k1", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := v1.Close(); err != nil {
		t.Fatal(err)
	}

	v2, err := secrets.NewFileVault(path, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v2.Get(ctx, "acme", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v1" {
		t.Errorf("after reload got %q want v1", got)
	}
}

func TestFileVault_DeleteAndList(t *testing.T) {
	dir := t.TempDir()
	v, err := secrets.NewFileVault(filepath.Join(dir, "vault.yaml"), newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := v.Put(ctx, "acme", "a", "1"); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(ctx, "acme", "b", "2"); err != nil {
		t.Fatal(err)
	}
	names, err := v.List(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("names = %v", names)
	}
	if err := v.Delete(ctx, "acme", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get(ctx, "acme", "a"); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
	if err := v.Delete(ctx, "acme", "missing"); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("delete missing should ErrNotFound, got %v", err)
	}
}

func TestFileVault_BadKeyLength(t *testing.T) {
	if _, err := secrets.NewFileVault("ignore", []byte("short")); err == nil {
		t.Error("expected error for short key")
	}
}
