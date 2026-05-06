package secrets_test

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/hurtener/Portico_gateway/internal/secrets"
)

// rawEntry mirrors the on-disk shape used by FileVault. We keep a
// duplicate inside the test package because the production type is
// unexported by design — tests that need to manipulate the YAML file
// directly (legacy fixtures, swap attacks, corruption simulation) work
// against this mirror so they break loudly if the on-disk schema drifts.
type rawEntry struct {
	Ciphertext string `yaml:"ct"`
	Nonce      string `yaml:"nonce"`
	Scheme     string `yaml:"scheme,omitempty"`
}

type rawLayout struct {
	Version int                            `yaml:"version"`
	Entries map[string]map[string]rawEntry `yaml:"entries"`
}

func readLayout(t *testing.T, path string) rawLayout {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vault file: %v", err)
	}
	var out rawLayout
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("parse vault yaml: %v", err)
	}
	return out
}

func writeLayout(t *testing.T, path string, layout rawLayout) {
	t.Helper()
	out, err := yaml.Marshal(&layout)
	if err != nil {
		t.Fatalf("marshal vault yaml: %v", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write vault file: %v", err)
	}
}

// encryptLegacy mirrors the pre-upgrade scheme: master key applied
// directly with AES-256-GCM and no AAD. Used by
// TestFileVault_LegacyDecryptStillWorks to seed a vault file that looks
// like one written before the HKDF upgrade.
func encryptLegacy(t *testing.T, key []byte, plaintext string) rawEntry {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatalf("rand: %v", err)
	}
	ct := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return rawEntry{
		Ciphertext: base64.StdEncoding.EncodeToString(ct),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		// Scheme intentionally empty: legacy entry.
	}
}

func TestFileVault_HKDF_PutGetRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.yaml")
	v, err := secrets.NewFileVault(path, newKey(t))
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

	// Confirm the on-disk entry was written under the v1 scheme.
	layout := readLayout(t, path)
	entry := layout.Entries["acme"]["github_token"]
	if entry.Scheme != "v1" {
		t.Errorf("expected scheme=v1 on disk, got %q", entry.Scheme)
	}
	if entry.Ciphertext == "" || entry.Nonce == "" {
		t.Errorf("ciphertext/nonce should be populated, got %+v", entry)
	}
}

// TestFileVault_HKDF_TenantNameBinding verifies the (tenant, name) binding:
// swapping ciphertexts between two entries (even within the same tenant
// or between tenants) must cause Get to fail because the AAD differs.
func TestFileVault_HKDF_TenantNameBinding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.yaml")
	key := newKey(t)
	v, err := secrets.NewFileVault(path, key)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Two entries we can swap on disk.
	if err := v.Put(ctx, "acme", "alpha", "secret-alpha"); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(ctx, "beta", "bravo", "secret-bravo"); err != nil {
		t.Fatal(err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	// Swap the ciphertexts on disk: acme/alpha now holds beta/bravo's
	// bytes and vice-versa. The HKDF-derived key alone would suffice to
	// brute-force them; AAD binding is the defence.
	layout := readLayout(t, path)
	a := layout.Entries["acme"]["alpha"]
	b := layout.Entries["beta"]["bravo"]
	layout.Entries["acme"]["alpha"] = b
	layout.Entries["beta"]["bravo"] = a
	writeLayout(t, path, layout)

	v2, err := secrets.NewFileVault(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v2.Get(ctx, "acme", "alpha"); err == nil {
		t.Error("expected decrypt failure after swap (acme/alpha), got nil")
	}
	if _, err := v2.Get(ctx, "beta", "bravo"); err == nil {
		t.Error("expected decrypt failure after swap (beta/bravo), got nil")
	}

	// And same-tenant swap.
	if err := v2.Put(ctx, "acme", "x", "value-x"); err != nil {
		t.Fatal(err)
	}
	if err := v2.Put(ctx, "acme", "y", "value-y"); err != nil {
		t.Fatal(err)
	}
	if err := v2.Close(); err != nil {
		t.Fatal(err)
	}
	layout = readLayout(t, path)
	x := layout.Entries["acme"]["x"]
	y := layout.Entries["acme"]["y"]
	layout.Entries["acme"]["x"] = y
	layout.Entries["acme"]["y"] = x
	writeLayout(t, path, layout)

	v3, err := secrets.NewFileVault(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v3.Get(ctx, "acme", "x"); err == nil {
		t.Error("expected decrypt failure after intra-tenant swap (acme/x)")
	}
	if _, err := v3.Get(ctx, "acme", "y"); err == nil {
		t.Error("expected decrypt failure after intra-tenant swap (acme/y)")
	}
}

func TestFileVault_HKDF_WrongMasterKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.yaml")
	keyA := newKey(t)
	keyB := newKey(t)

	vA, err := secrets.NewFileVault(path, keyA)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := vA.Put(ctx, "acme", "k", "plain-A"); err != nil {
		t.Fatal(err)
	}
	if err := vA.Close(); err != nil {
		t.Fatal(err)
	}

	vB, err := secrets.NewFileVault(path, keyB)
	if err != nil {
		t.Fatal(err)
	}
	got, err := vB.Get(ctx, "acme", "k")
	if err == nil {
		t.Fatalf("expected error opening with wrong key, got plaintext=%q", got)
	}
	if got != "" {
		t.Errorf("plaintext should be empty on error, got %q", got)
	}
}

func TestFileVault_LegacyDecryptStillWorks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.yaml")
	key := newKey(t)

	// Hand-write a legacy-format vault file: no Scheme tag, ciphertext
	// produced by the master key applied directly with no AAD.
	legacy := encryptLegacy(t, key, "legacy-value")
	writeLayout(t, path, rawLayout{
		Version: 1,
		Entries: map[string]map[string]rawEntry{
			"acme": {"legacy_token": legacy},
		},
	})

	v, err := secrets.NewFileVault(path, key)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	got, err := v.Get(ctx, "acme", "legacy_token")
	if err != nil {
		t.Fatalf("legacy decrypt failed: %v", err)
	}
	if got != "legacy-value" {
		t.Errorf("got %q want legacy-value", got)
	}

	// A subsequent Put rewrites the entry under v1.
	if err := v.Put(ctx, "acme", "legacy_token", "rewritten"); err != nil {
		t.Fatal(err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	layout := readLayout(t, path)
	if got := layout.Entries["acme"]["legacy_token"].Scheme; got != "v1" {
		t.Errorf("after Put, scheme should be v1, got %q", got)
	}
}

func TestFileVault_RotateKey_PreservesValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.yaml")
	keyA := newKey(t)
	keyB := newKey(t)

	v, err := secrets.NewFileVault(path, keyA)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	want := map[string]map[string]string{
		"acme": {"k1": "v1", "k2": "v2", "k3": "v3"},
		"beta": {"alpha": "AAA", "bravo": "BBB"},
	}
	for tenant, entries := range want {
		for name, value := range entries {
			if err := v.Put(ctx, tenant, name, value); err != nil {
				t.Fatalf("put %s/%s: %v", tenant, name, err)
			}
		}
	}

	if err := v.RotateKey(ctx, keyB); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// Reads against the rotated vault use the new key (in-memory).
	for tenant, entries := range want {
		for name, value := range entries {
			got, err := v.Get(ctx, tenant, name)
			if err != nil {
				t.Errorf("post-rotate get %s/%s: %v", tenant, name, err)
				continue
			}
			if got != value {
				t.Errorf("post-rotate %s/%s = %q want %q", tenant, name, got, value)
			}
		}
	}

	// Closing and reopening with the OLD key must fail; with the NEW
	// key must succeed.
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	vOld, err := secrets.NewFileVault(path, keyA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vOld.Get(ctx, "acme", "k1"); err == nil {
		t.Error("expected decrypt failure with old key after rotate")
	}

	vNew, err := secrets.NewFileVault(path, keyB)
	if err != nil {
		t.Fatal(err)
	}
	for tenant, entries := range want {
		for name, value := range entries {
			got, err := vNew.Get(ctx, tenant, name)
			if err != nil {
				t.Errorf("reopened-with-new-key get %s/%s: %v", tenant, name, err)
				continue
			}
			if got != value {
				t.Errorf("reopened %s/%s = %q want %q", tenant, name, got, value)
			}
		}
	}
}

// TestFileVault_RotateKey_IsAtomic corrupts one entry's ciphertext on
// disk, then attempts a key rotation. The rotation must fail and leave
// the file in its pre-rotate state — verified by reading every other
// (uncorrupted) entry back with the OLD key after the failed rotation.
func TestFileVault_RotateKey_IsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.yaml")
	keyA := newKey(t)
	keyB := newKey(t)

	v, err := secrets.NewFileVault(path, keyA)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := v.Put(ctx, "acme", "good1", "value-good1"); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(ctx, "acme", "broken", "value-broken"); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(ctx, "beta", "good2", "value-good2"); err != nil {
		t.Fatal(err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	// Snapshot of the pre-corruption file for byte-equality check after
	// the failed rotation.
	preCorruption, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Corrupt one entry's ciphertext on disk. The HKDF derivation still
	// runs, but Open will fail because the GCM tag won't verify.
	layout := readLayout(t, path)
	corrupted := layout.Entries["acme"]["broken"]
	rawCT, err := base64.StdEncoding.DecodeString(corrupted.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	rawCT[0] ^= 0xFF
	corrupted.Ciphertext = base64.StdEncoding.EncodeToString(rawCT)
	layout.Entries["acme"]["broken"] = corrupted
	writeLayout(t, path, layout)

	postCorruption, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Reopen and attempt rotation. Because one entry won't decrypt, the
	// rotation must abort — the file must equal postCorruption (i.e.
	// the rotation did not write anything new), and the surviving
	// entries must still be readable with the OLD key.
	v2, err := secrets.NewFileVault(path, keyA)
	if err != nil {
		t.Fatal(err)
	}
	if err := v2.RotateKey(ctx, keyB); err == nil {
		t.Fatal("expected RotateKey to fail when an entry can't be decrypted")
	}

	afterFailedRotate, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterFailedRotate) != string(postCorruption) {
		t.Errorf("file changed despite failed rotation\nbefore=%q\nafter=%q",
			postCorruption, afterFailedRotate)
	}

	// Sanity: pre-corruption file is not byte-equal to post-corruption,
	// otherwise our test setup would be lying.
	if string(preCorruption) == string(postCorruption) {
		t.Fatal("corruption did not modify file: test setup is broken")
	}

	// Surviving entries must still be readable with the OLD key — i.e.
	// the in-memory key was NOT rotated despite the failed call.
	got1, err := v2.Get(ctx, "acme", "good1")
	if err != nil {
		t.Errorf("get acme/good1 after failed rotate: %v", err)
	} else if got1 != "value-good1" {
		t.Errorf("acme/good1 = %q want value-good1", got1)
	}
	got2, err := v2.Get(ctx, "beta", "good2")
	if err != nil {
		t.Errorf("get beta/good2 after failed rotate: %v", err)
	} else if got2 != "value-good2" {
		t.Errorf("beta/good2 = %q want value-good2", got2)
	}

	// And the broken entry should report a decrypt error rather than
	// silent garbage.
	if _, err := v2.Get(ctx, "acme", "broken"); err == nil {
		t.Error("expected get on corrupted entry to fail")
	} else if errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("got ErrNotFound, want decrypt error: %v", err)
	}
}
