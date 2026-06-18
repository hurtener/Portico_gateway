// Package virtualkeys implements Portico-side Virtual Keys (pk-portico-*): a
// programmatic, HMAC-bound credential that resolves to a tenant + scope set +
// provider/model/MCP allowlists + (optionally) an Agent Profile and a budget
// hierarchy. Secrets are never stored — only a per-VK salt and the HMAC of the
// secret, so a leaked database cannot reconstruct a usable key, and a leaked
// key for one VK cannot authenticate as another (the secret is verified against
// that VK's stored HMAC, and the VK id determines the tenant).
package virtualkeys

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
)

// TokenPrefix is the fixed, recognisable prefix every Virtual Key carries. The
// auth middleware uses it to route a Bearer token to the VK resolver instead of
// the JWT validator. It deliberately differs from provider keys (sk-…).
const TokenPrefix = "pk-portico-"

// idPrefix marks the VK id segment embedded in a token (and used as the row id).
const idPrefix = "vk_"

// tokenSep separates the VK id from the secret inside a token. Neither segment
// contains it (id is idPrefix+hex, secret is base62), so the split is exact.
const tokenSep = "."

// base62 alphabet for the secret. No separator/prefix chars appear in it.
const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// secretBytes is the entropy of the secret before base62 encoding (~40 chars).
const secretBytes = 30

// ErrMalformedToken is returned when a bearer string is not a well-formed VK
// token. The resolver never hits the DB on a malformed token.
var ErrMalformedToken = errors.New("virtualkeys: malformed token")

// NewID returns a fresh globally-unique VK id ("vk_" + 24 hex chars / 96 bits).
func NewID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return idPrefix + hex.EncodeToString(b), nil
}

// newSecret returns a fresh random base62 secret (~40 chars, 240 bits entropy).
func newSecret() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = base62[int(v)%len(base62)]
	}
	return string(out), nil
}

// NewSalt returns a fresh 16-byte random salt for HMAC binding.
func NewSalt() ([]byte, error) {
	s := make([]byte, 16)
	if _, err := rand.Read(s); err != nil {
		return nil, err
	}
	return s, nil
}

// ComputeHMAC returns HMAC-SHA256(key=salt, msg=secret). Storing salt + this
// value (never the secret) is the VK binding. Verification recomputes and
// compares in constant time.
func ComputeHMAC(salt []byte, secret string) []byte {
	mac := hmac.New(sha256.New, salt)
	mac.Write([]byte(secret))
	return mac.Sum(nil)
}

// VerifyHMAC reports whether secret matches the stored salt+hmac, in constant
// time (resists timing oracles).
func VerifyHMAC(salt, storedHMAC []byte, secret string) bool {
	want := ComputeHMAC(salt, secret)
	return subtle.ConstantTimeCompare(want, storedHMAC) == 1
}

// ComposeToken builds the user-facing token from a VK id + secret:
// "pk-portico-<id>.<secret>". Shown to the operator exactly once.
func ComposeToken(id, secret string) string {
	return TokenPrefix + id + tokenSep + secret
}

// LooksLikeVK reports whether a bearer string carries the VK prefix. Cheap; used
// by the auth middleware to decide VK vs JWT before any parsing.
func LooksLikeVK(bearer string) bool {
	return strings.HasPrefix(bearer, TokenPrefix)
}

// ParseToken splits a token into its VK id and secret. It validates the prefix,
// the id marker, and that both segments are non-empty — without any DB access,
// so malformed input is rejected cheaply. The returned secret is sensitive.
func ParseToken(token string) (id, secret string, err error) {
	if !strings.HasPrefix(token, TokenPrefix) {
		return "", "", ErrMalformedToken
	}
	rest := token[len(TokenPrefix):]
	i := strings.Index(rest, tokenSep)
	if i <= 0 || i >= len(rest)-1 {
		return "", "", ErrMalformedToken
	}
	id = rest[:i]
	secret = rest[i+1:]
	if !strings.HasPrefix(id, idPrefix) || len(id) <= len(idPrefix) {
		return "", "", ErrMalformedToken
	}
	if secret == "" {
		return "", "", ErrMalformedToken
	}
	return id, secret, nil
}
