package secrets

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Phase 9 follow-up: lift internal/secrets coverage past the 80% gate by
// covering reveal-edge paths and the env-key loader.

func TestRevealManager_DoubleConsumeRejected(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()
	if err := v.Put(ctx, "acme", "k1", "v"); err != nil {
		t.Fatal(err)
	}
	mgr := NewRevealManager(v, nil)
	tok, err := mgr.IssueRevealToken(ctx, "acme", "k1", "ops")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := mgr.ConsumeReveal(ctx, tok.Token); err != nil {
		t.Fatal(err)
	}
	// Second consume must fail (single-use is non-negotiable).
	if _, _, _, _, err := mgr.ConsumeReveal(ctx, tok.Token); err == nil {
		t.Errorf("second consume should fail")
	}
}

func TestRevealManager_VaultMissingSecretAfterIssue(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()
	if err := v.Put(ctx, "acme", "k1", "v"); err != nil {
		t.Fatal(err)
	}
	mgr := NewRevealManager(v, nil)
	tok, err := mgr.IssueRevealToken(ctx, "acme", "k1", "ops")
	if err != nil {
		t.Fatal(err)
	}
	// Delete the underlying secret so the consume path hits the vault Get
	// failure branch.
	if err := v.Delete(ctx, "acme", "k1"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := mgr.ConsumeReveal(ctx, tok.Token); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after underlying secret deleted, got %v", err)
	}
}

func TestRevealManager_EmptyToken(t *testing.T) {
	mgr := NewRevealManager(newTestVault(t), nil)
	if _, _, _, _, err := mgr.ConsumeReveal(context.Background(), ""); err == nil {
		t.Errorf("expected error for empty token")
	}
}

func TestRevealManager_NilManagerRejected(t *testing.T) {
	var mgr *RevealManager
	if _, err := mgr.IssueRevealToken(context.Background(), "t", "n", "u"); err == nil {
		t.Errorf("expected error from nil manager")
	}
	if _, _, _, _, err := mgr.ConsumeReveal(context.Background(), "x"); err == nil {
		t.Errorf("expected error from nil manager consume")
	}
}

func TestRevealManager_TenantOrNameRequired(t *testing.T) {
	mgr := NewRevealManager(newTestVault(t), nil)
	if _, err := mgr.IssueRevealToken(context.Background(), "", "n", "u"); err == nil {
		t.Errorf("expected error for empty tenant")
	}
	if _, err := mgr.IssueRevealToken(context.Background(), "t", "", "u"); err == nil {
		t.Errorf("expected error for empty name")
	}
}

func TestRevealManager_GcExpiresOnIssue(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()
	if err := v.Put(ctx, "acme", "a", "1"); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(ctx, "acme", "b", "2"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	clock := &mockClock{t: now}
	mgr := NewRevealManager(v, clock.now)

	// Issue a token, advance the clock past TTL, then issue another. The
	// gcLocked path should sweep the expired entry on the second issue.
	if _, err := mgr.IssueRevealToken(ctx, "acme", "a", "u"); err != nil {
		t.Fatal(err)
	}
	clock.t = now.Add(2 * RevealTokenTTL)
	if _, err := mgr.IssueRevealToken(ctx, "acme", "b", "u"); err != nil {
		t.Fatal(err)
	}
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	if len(mgr.tokens) != 1 {
		t.Errorf("expected gcLocked to drop expired token, got %d entries", len(mgr.tokens))
	}
}

// LoadKeyFromEnv coverage — env unset, env set valid, env set invalid.
func TestLoadKeyFromEnv_Unset(t *testing.T) {
	t.Setenv("PORTICO_VAULT_KEY", "")
	key, err := LoadKeyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if key != nil {
		t.Errorf("expected nil key when env unset")
	}
}

func TestLoadKeyFromEnv_ValidB64(t *testing.T) {
	// Documented dummy key: 32 zero bytes per CLAUDE.md §7.2.
	keyB64 := base64.StdEncoding.EncodeToString(make([]byte, 32))
	t.Setenv("PORTICO_VAULT_KEY", keyB64)
	key, err := LoadKeyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Errorf("want 32 bytes, got %d", len(key))
	}
}

func TestLoadKeyFromEnv_InvalidB64(t *testing.T) {
	t.Setenv("PORTICO_VAULT_KEY", "@@@not-base64@@@")
	if _, err := LoadKeyFromEnv(); err == nil {
		t.Errorf("expected base64 decode error")
	}
}

func TestLoadKeyFromEnv_WrongLength(t *testing.T) {
	t.Setenv("PORTICO_VAULT_KEY", base64.StdEncoding.EncodeToString([]byte("short")))
	if _, err := LoadKeyFromEnv(); err == nil {
		t.Errorf("expected wrong-length error")
	}
}

// FileVault edge paths: empty path is a no-op for load and flush.
func TestFileVault_EmptyPath_NoOp(t *testing.T) {
	key := make([]byte, 32)
	v, err := NewFileVault("", key)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	ctx := context.Background()
	if err := v.Put(ctx, "t", "k", "v"); err != nil {
		t.Fatal(err)
	}
	got, err := v.Get(ctx, "t", "k")
	if err != nil {
		t.Fatal(err)
	}
	if got != "v" {
		t.Errorf("in-memory roundtrip failed: %q", got)
	}
}

// Get/Put/Delete reject empty tenant or name.
func TestFileVault_EmptyTenantOrName(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()
	if _, err := v.Get(ctx, "", "n"); err == nil {
		t.Errorf("expected error for empty tenant on Get")
	}
	if _, err := v.Get(ctx, "t", ""); err == nil {
		t.Errorf("expected error for empty name on Get")
	}
	if err := v.Put(ctx, "", "n", "v"); err == nil {
		t.Errorf("expected error for empty tenant on Put")
	}
	if err := v.Put(ctx, "t", "", "v"); err == nil {
		t.Errorf("expected error for empty name on Put")
	}
}

// Cancelled context paths.
func TestFileVault_ContextCancelled(t *testing.T) {
	v := newTestVault(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := v.Get(ctx, "t", "n"); err == nil {
		t.Errorf("expected error from cancelled ctx")
	}
	if err := v.Put(ctx, "t", "n", "v"); err == nil {
		t.Errorf("expected error from cancelled ctx Put")
	}
	if err := v.Delete(ctx, "t", "n"); err == nil {
		t.Errorf("expected error from cancelled ctx Delete")
	}
	if _, err := v.List(ctx, "t"); err == nil {
		t.Errorf("expected error from cancelled ctx List")
	}
	if err := v.RotateKey(ctx, make([]byte, 32)); err == nil {
		t.Errorf("expected error from cancelled ctx RotateKey")
	}
}

// load() handles a non-existent file, an empty file, and a corrupt YAML.
func TestFileVault_LoadCorruptYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.yaml")
	if err := os.WriteFile(path, []byte("\xff: not yaml ::"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileVault(path, make([]byte, 32)); err == nil {
		t.Errorf("expected parse error on corrupt YAML")
	}
}

// load() returns nil cleanly when the file is empty (whitespace only).
func TestFileVault_LoadEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.yaml")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileVault(path, make([]byte, 32)); err != nil {
		t.Errorf("empty file should be a no-op: %v", err)
	}
}

// RotateKey rejects keys that aren't 32 bytes.
func TestFileVault_RotateKeyBadLength(t *testing.T) {
	v := newTestVault(t)
	if err := v.RotateKey(context.Background(), []byte("short")); err == nil {
		t.Errorf("expected error for short rotate key")
	}
}

// flushLayout error path: directory creation under an unwritable parent.
// Use a path component that cannot be a directory (the yaml file itself).
func TestFileVault_FlushUnderRegularFileFails(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	// This path tries to create a dir at "regular/sub" but "regular" is a
	// regular file, so MkdirAll fails.
	v, err := NewFileVault(filepath.Join(regular, "sub", "vault.yaml"), make([]byte, 32))
	if err != nil {
		// The constructor calls load() which is a no-op on missing path.
		// flush is only called from Put.
		_ = err
		return
	}
	if err := v.Put(context.Background(), "t", "n", "v"); err == nil {
		t.Errorf("expected flush to fail under regular file")
	}
}
