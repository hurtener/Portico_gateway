package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
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

// fileEntry is the on-disk shape: ciphertext + nonce, base64-encoded.
type fileEntry struct {
	Ciphertext string `yaml:"ct"`
	Nonce      string `yaml:"nonce"`
}

// fileLayout is the entire vault file contents:
//
//	version: 1
//	entries:
//	  acme:
//	    github_token:
//	      ct: ...
//	      nonce: ...
type fileLayout struct {
	Version int                             `yaml:"version"`
	Entries map[string]map[string]fileEntry `yaml:"entries"`
}

// FileVault stores secrets in a YAML file at Path, encrypted per-value with
// AES-256-GCM using the master key from PORTICO_VAULT_KEY (base64).
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
	v := &FileVault{path: path, key: key, data: fileLayout{Version: 1, Entries: map[string]map[string]fileEntry{}}}
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
	return v.decrypt(e)
}

// Put stores or replaces the secret. Writes the file atomically (temp +
// rename) so a crash mid-write never produces a partial file.
func (v *FileVault) Put(ctx context.Context, tenantID, name, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" || name == "" {
		return errors.New("vault: tenant id and name are required")
	}
	entry, err := v.encrypt(value)
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

// Close releases any resources. The file vault holds no live handles.
func (v *FileVault) Close() error { return nil }

// ----- internals ----------------------------------------------------------

func (v *FileVault) encrypt(plaintext string) (fileEntry, error) {
	block, err := aes.NewCipher(v.key)
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
	ct := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return fileEntry{
		Ciphertext: base64.StdEncoding.EncodeToString(ct),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
	}, nil
}

func (v *FileVault) decrypt(e fileEntry) (string, error) {
	ct, err := base64.StdEncoding.DecodeString(e.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("vault: decode ct: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(e.Nonce)
	if err != nil {
		return "", fmt.Errorf("vault: decode nonce: %w", err)
	}
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	pt, err := gcm.Open(nil, nonce, ct, nil)
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
	if v.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(v.path), 0o700); err != nil {
		return err
	}
	out, err := yaml.Marshal(&v.data)
	if err != nil {
		return err
	}
	tmp := v.path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, v.path)
}

// Compile-time check that FileVault implements Vault.
var _ Vault = (*FileVault)(nil)
