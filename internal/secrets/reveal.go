package secrets

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// RevealToken is the one-shot token returned by IssueRevealToken. The
// token is 256-bit random, single-use, and expires after RevealTokenTTL.
type RevealToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// RevealTokenTTL is the lifetime of a reveal token. Short by design — the
// token is meant for an immediate copy-to-clipboard, not for batch jobs.
const RevealTokenTTL = 60 * time.Second

// RevealManager wraps a Vault and issues short-lived reveal tokens. The
// REST handlers consume it via the AuditEmitter so issue/consume events
// are recorded. Storage is in-memory by design — restart drops every
// pending token, which is the correct behaviour.
type RevealManager struct {
	vault Vault
	clock func() time.Time

	mu     sync.Mutex
	tokens map[string]revealEntry
}

type revealEntry struct {
	tenant    string
	name      string
	actorID   string
	expiresAt time.Time
}

// NewRevealManager constructs a manager. clock may be nil (defaults to
// time.Now); it is exposed for tests.
func NewRevealManager(v Vault, clock func() time.Time) *RevealManager {
	if clock == nil {
		clock = func() time.Time { return time.Now() }
	}
	return &RevealManager{
		vault:  v,
		clock:  clock,
		tokens: make(map[string]revealEntry),
	}
}

// IssueRevealToken returns a fresh token bound to (tenant, name, actor).
// The token is never persisted to disk.
func (m *RevealManager) IssueRevealToken(ctx context.Context, tenant, name, actorID string) (RevealToken, error) {
	if err := ctx.Err(); err != nil {
		return RevealToken{}, err
	}
	if m == nil || m.vault == nil {
		return RevealToken{}, errors.New("secrets: reveal manager not configured")
	}
	if tenant == "" || name == "" {
		return RevealToken{}, errors.New("secrets: tenant and name required")
	}
	// Confirm the secret exists before issuing — otherwise the token is
	// useless and we leak the (tenant, name) probe.
	if _, err := m.vault.Get(ctx, tenant, name); err != nil {
		return RevealToken{}, err
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return RevealToken{}, err
	}
	token := base64.URLEncoding.EncodeToString(raw[:])
	expires := m.clock().Add(RevealTokenTTL)

	m.mu.Lock()
	m.gcLocked()
	m.tokens[token] = revealEntry{
		tenant:    tenant,
		name:      name,
		actorID:   actorID,
		expiresAt: expires,
	}
	m.mu.Unlock()

	return RevealToken{Token: token, ExpiresAt: expires}, nil
}

// ConsumeReveal returns the plaintext for a valid, unexpired token. The
// token is invalidated whether the lookup succeeds or fails — single-use
// is non-negotiable.
func (m *RevealManager) ConsumeReveal(ctx context.Context, token string) (plaintext, tenant, name, actor string, err error) {
	if err = ctx.Err(); err != nil {
		return "", "", "", "", err
	}
	if m == nil || m.vault == nil {
		return "", "", "", "", errors.New("secrets: reveal manager not configured")
	}
	if token == "" {
		return "", "", "", "", errors.New("secrets: empty reveal token")
	}

	m.mu.Lock()
	// Constant-time scan so the response time doesn't leak whether a
	// token exists.
	var found revealEntry
	hit := false
	for k, v := range m.tokens {
		if subtle.ConstantTimeCompare([]byte(k), []byte(token)) == 1 {
			found = v
			delete(m.tokens, k)
			hit = true
		}
	}
	m.mu.Unlock()

	if !hit {
		return "", "", "", "", errors.New("secrets: reveal token unknown")
	}
	if m.clock().After(found.expiresAt) {
		return "", "", "", "", errors.New("secrets: reveal token expired")
	}
	pt, err := m.vault.Get(ctx, found.tenant, found.name)
	if err != nil {
		return "", "", "", "", err
	}
	return pt, found.tenant, found.name, found.actorID, nil
}

// gcLocked removes expired entries. Called under m.mu.
func (m *RevealManager) gcLocked() {
	now := m.clock()
	for k, v := range m.tokens {
		if now.After(v.expiresAt) {
			delete(m.tokens, k)
		}
	}
}
