package secrets

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestVault(t *testing.T) *FileVault {
	t.Helper()
	dir := t.TempDir()
	// Documented dummy key per CLAUDE.md §7.2 — base64 of 32 zero bytes.
	keyB64 := base64.StdEncoding.EncodeToString(make([]byte, 32))
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewFileVault(filepath.Join(dir, "vault.yaml"), key)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestReveal_TokenSingleUse(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()
	if err := v.Put(ctx, "acme", "k1", "secret-value"); err != nil {
		t.Fatal(err)
	}
	mgr := NewRevealManager(v, nil)
	tok, err := mgr.IssueRevealToken(ctx, "acme", "k1", "ops@acme")
	if err != nil {
		t.Fatal(err)
	}
	pt, _, _, _, err := mgr.ConsumeReveal(ctx, tok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if pt != "secret-value" {
		t.Errorf("plaintext mismatch: %q", pt)
	}
	// Second consume must fail.
	if _, _, _, _, err := mgr.ConsumeReveal(ctx, tok.Token); err == nil {
		t.Errorf("second consume should fail")
	}
}

func TestReveal_TokenExpires(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()
	if err := v.Put(ctx, "acme", "k1", "x"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	clock := &mockClock{t: now}
	mgr := NewRevealManager(v, clock.now)
	tok, err := mgr.IssueRevealToken(ctx, "acme", "k1", "ops")
	if err != nil {
		t.Fatal(err)
	}
	clock.t = now.Add(2 * time.Minute)
	if _, _, _, _, err := mgr.ConsumeReveal(ctx, tok.Token); err == nil ||
		!strings.Contains(err.Error(), "expired") {
		t.Errorf("expected expired error, got %v", err)
	}
}

func TestReveal_RejectsUnknownToken(t *testing.T) {
	v := newTestVault(t)
	mgr := NewRevealManager(v, nil)
	if _, _, _, _, err := mgr.ConsumeReveal(context.Background(), "bogus"); err == nil {
		t.Errorf("expected error for unknown token")
	}
}

func TestReveal_RejectsMissingSecret(t *testing.T) {
	v := newTestVault(t)
	mgr := NewRevealManager(v, nil)
	if _, err := mgr.IssueRevealToken(context.Background(), "acme", "missing", "ops"); err == nil {
		t.Errorf("expected error issuing token for missing secret")
	}
}

type mockClock struct{ t time.Time }

func (m *mockClock) now() time.Time { return m.t }
