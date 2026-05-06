package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// schemeV1 marks an entry encrypted with the v1 scheme: a per-value key
// derived from the master key via HKDF-SHA256(info="portico/v1/<tenant>/<name>"),
// AES-256-GCM with the (tenant, name) tuple bound as Additional Authenticated
// Data, and a fresh 12-byte nonce. An empty/missing Scheme means the entry was
// written by the legacy scheme (master key applied directly with no AAD) and
// must still decrypt during the migration window.
const schemeV1 = "v1"

// hkdfInfoPrefix is the domain-separation tag mixed into every per-value key
// derivation. Bumping it (e.g. portico/v2/) is how a future scheme upgrade
// avoids colliding with v1 derivations from the same master key.
const hkdfInfoPrefix = "portico/v1/"

// fileEntry is the on-disk shape: ciphertext + nonce + scheme tag, all
// base64-encoded except scheme. Scheme is empty for legacy entries written
// before the HKDF/AAD upgrade; new writes always populate it.
type fileEntry struct {
	Ciphertext string `yaml:"ct"`
	Nonce      string `yaml:"nonce"`
	Scheme     string `yaml:"scheme,omitempty"`
}

// fileLayout is the entire vault file contents:
//
//	version: 1
//	entries:
//	  acme:
//	    github_token:
//	      ct: ...
//	      nonce: ...
//	      scheme: v1
type fileLayout struct {
	Version int                             `yaml:"version"`
	Entries map[string]map[string]fileEntry `yaml:"entries"`
}

// FileVault stores secrets in a YAML file at Path, encrypted per-value with
// AES-256-GCM. Entries written by the v1 scheme use a per-value key derived
// from the master key via HKDF-SHA256, with the (tenant, name) tuple bound
// as Additional Authenticated Data. Older entries (no Scheme tag) are
// decrypted with the master key directly during the transition window;
// every Put rewrites them under v1 on next write.
type FileVault struct {
	path string
	key  []byte
	mu   sync.RWMutex
	data fileLayout
}

// NewFileVault opens (or creates) a vault file. If the file does not exist
// an empty vault is returned. The master key must be exactly 32 bytes.
func NewFileVault(path string, key []byte) (*FileVault, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("vault: master key must be 32 bytes, got %d", len(key))
	}
	v := &FileVault{path: path, key: append([]byte(nil), key...), data: fileLayout{Version: 1, Entries: map[string]map[string]fileEntry{}}}
	if err := v.load(); err != nil {
		return nil, err
	}
	return v, nil
}

// LoadKeyFromEnv decodes PORTICO_VAULT_KEY (base64) into a 32-byte key.
// Returns nil, nil when the env var is unset so callers can fall back to
// disabled-vault behaviour.
func LoadKeyFromEnv() ([]byte, error) {
	raw := os.Getenv("PORTICO_VAULT_KEY")
	if raw == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("vault: PORTICO_VAULT_KEY: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("vault: PORTICO_VAULT_KEY must decode to 32 bytes (got %d)", len(key))
	}
	return key, nil
}

// Get returns the plaintext for (tenant, name).
func (v *FileVault) Get(ctx context.Context, tenantID, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if tenantID == "" || name == "" {
		return "", errors.New("vault: tenant id and name are required")
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	entries, ok := v.data.Entries[tenantID]
	if !ok {
		return "", ErrNotFound
	}
	e, ok := entries[name]
	if !ok {
		return "", ErrNotFound
	}
	return v.decryptEntry(tenantID, name, e, v.key)
}

// Put stores or replaces the secret. Writes the file atomically (temp +
// rename) so a crash mid-write never produces a partial file. New writes
// always use the v1 scheme (HKDF-derived per-value key + AAD binding).
func (v *FileVault) Put(ctx context.Context, tenantID, name, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" || name == "" {
		return errors.New("vault: tenant id and name are required")
	}
	entry, err := v.encryptV1(tenantID, name, value, v.key)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data.Entries == nil {
		v.data.Entries = map[string]map[string]fileEntry{}
	}
	if _, ok := v.data.Entries[tenantID]; !ok {
		v.data.Entries[tenantID] = map[string]fileEntry{}
	}
	v.data.Entries[tenantID][name] = entry
	return v.flush()
}

// Delete removes a secret. ErrNotFound when absent.
func (v *FileVault) Delete(ctx context.Context, tenantID, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	entries, ok := v.data.Entries[tenantID]
	if !ok {
		return ErrNotFound
	}
	if _, ok := entries[name]; !ok {
		return ErrNotFound
	}
	delete(entries, name)
	return v.flush()
}

// List returns the names of secrets the tenant owns, alphabetically.
func (v *FileVault) List(ctx context.Context, tenantID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	entries, ok := v.data.Entries[tenantID]
	if !ok {
		return []string{}, nil
	}
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// RotateKey decrypts every entry under the current master key (legacy or v1)
// and re-encrypts under newKey using the v1 scheme. The on-disk file is
// rewritten atomically; the in-memory state and key are only updated after
// the rename succeeds. Any per-entry decrypt failure aborts the rotation
// and leaves the file unchanged.
func (v *FileVault) RotateKey(ctx context.Context, newKey []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(newKey) != 32 {
		return fmt.Errorf("vault: new master key must be 32 bytes, got %d", len(newKey))
	}
	v.mu.Lock()
	defer v.mu.Unlock()

	// Build the rotated layout in memory first so a per-entry failure
	// aborts before we touch the filesystem.
	rotated := fileLayout{
		Version: v.data.Version,
		Entries: make(map[string]map[string]fileEntry, len(v.data.Entries)),
	}
	if rotated.Version == 0 {
		rotated.Version = 1
	}
	for tenantID, entries := range v.data.Entries {
		newEntries := make(map[string]fileEntry, len(entries))
		for name, e := range entries {
			plaintext, err := v.decryptEntry(tenantID, name, e, v.key)
			if err != nil {
				return fmt.Errorf("vault: rotate decrypt %s/%s: %w", tenantID, name, err)
			}
			next, err := v.encryptV1(tenantID, name, plaintext, newKey)
			if err != nil {
				return fmt.Errorf("vault: rotate encrypt %s/%s: %w", tenantID, name, err)
			}
			newEntries[name] = next
		}
		rotated.Entries[tenantID] = newEntries
	}

	// Persist atomically. Until rename succeeds, the file is untouched.
	if err := flushLayout(v.path, &rotated); err != nil {
		return err
	}
	v.data = rotated
	v.key = append([]byte(nil), newKey...)
	return nil
}

// Close releases any resources. The file vault holds no live handles.
func (v *FileVault) Close() error { return nil }

// ----- internals ----------------------------------------------------------

// deriveKey derives a 32-byte AES-256 key from masterKey for a specific
// (tenant, name) pair using HKDF-SHA256. The salt is empty by design; the
// info parameter ("portico/v1/<tenant>/<name>") provides domain separation
// so that no two (tenant, name) pairs ever share a derived key, even with
// the same master key.
func deriveKey(masterKey []byte, tenantID, name string) ([]byte, error) {
	info := hkdfInfoPrefix + tenantID + "/" + name
	return hkdf.Key(sha256.New, masterKey, nil, info, 32)
}

// aadFor returns the additional authenticated data bound into the GCM tag
// for entries written under the v1 scheme. Swapping a ciphertext between
// (tenant, name) pairs causes Open to fail because this AAD changes.
func aadFor(tenantID, name string) []byte {
	return []byte(tenantID + "/" + name)
}

// encryptV1 encrypts plaintext for (tenant, name) under masterKey using the
// v1 scheme: HKDF-derived per-value key, fresh 12-byte nonce, AES-256-GCM
// with the (tenant, name) tuple bound as AAD.
func (v *FileVault) encryptV1(tenantID, name, plaintext string, masterKey []byte) (fileEntry, error) {
	dk, err := deriveKey(masterKey, tenantID, name)
	if err != nil {
		return fileEntry{}, fmt.Errorf("vault: hkdf: %w", err)
	}
	block, err := aes.NewCipher(dk)
	if err != nil {
		return fileEntry{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fileEntry{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fileEntry{}, err
	}
	ct := gcm.Seal(nil, nonce, []byte(plaintext), aadFor(tenantID, name))
	return fileEntry{
		Ciphertext: base64.StdEncoding.EncodeToString(ct),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Scheme:     schemeV1,
	}, nil
}

// decryptEntry routes to the v1 or legacy decrypt path based on the entry's
// Scheme tag. Empty/missing Scheme means a legacy (master-key-direct, no
// AAD) entry written before the upgrade.
func (v *FileVault) decryptEntry(tenantID, name string, e fileEntry, masterKey []byte) (string, error) {
	ct, err := base64.StdEncoding.DecodeString(e.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("vault: decode ct: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(e.Nonce)
	if err != nil {
		return "", fmt.Errorf("vault: decode nonce: %w", err)
	}

	switch e.Scheme {
	case schemeV1:
		dk, err := deriveKey(masterKey, tenantID, name)
		if err != nil {
			return "", fmt.Errorf("vault: hkdf: %w", err)
		}
		return decryptGCM(dk, nonce, ct, aadFor(tenantID, name))
	case "":
		// Legacy: master key applied directly, no AAD.
		return decryptGCM(masterKey, nonce, ct, nil)
	default:
		return "", fmt.Errorf("vault: unknown scheme %q", e.Scheme)
	}
}

// decryptGCM is a thin AES-256-GCM Open helper shared by the v1 and legacy
// paths.
func decryptGCM(key, nonce, ct, aad []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	pt, err := gcm.Open(nil, nonce, ct, aad)
	if err != nil {
		return "", fmt.Errorf("vault: decrypt: %w", err)
	}
	return string(pt), nil
}

func (v *FileVault) load() error {
	if v.path == "" {
		return nil
	}
	data, err := os.ReadFile(v.path) //nolint:gosec // operator-supplied path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("vault: read %s: %w", v.path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := yaml.Unmarshal(data, &v.data); err != nil {
		return fmt.Errorf("vault: parse %s: %w", v.path, err)
	}
	if v.data.Version == 0 {
		v.data.Version = 1
	}
	if v.data.Entries == nil {
		v.data.Entries = map[string]map[string]fileEntry{}
	}
	return nil
}

func (v *FileVault) flush() error {
	return flushLayout(v.path, &v.data)
}

// flushLayout writes layout to path atomically (temp + rename). Used by
// Put/Delete/RotateKey so the file is either fully old or fully new — never
// half-written.
func flushLayout(path string, layout *fileLayout) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	out, err := yaml.Marshal(layout)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Compile-time check that FileVault implements Vault.
var _ Vault = (*FileVault)(nil)
