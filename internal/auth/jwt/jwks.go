package jwt

import (
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"sync"
	"time"
)

// KeySet abstracts JWKS lookup. Phase 0 implements two backends: static and
// remote. Both are read-only after construction except for remote refresh.
type KeySet interface {
	// LookupKey returns the public key for the given key id (kid). Algorithm is
	// hint-only; KeySet may ignore it.
	LookupKey(ctx context.Context, kid string) (any, error)
}

// jwk is a minimal subset of the JWK spec covering RSA (RS256/384/512) and EC
// (ES256/384/512). HS* (symmetric) keys are intentionally not parsed — they
// are rejected at validator level.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use,omitempty"`
	Alg string `json:"alg,omitempty"`
	N   string `json:"n,omitempty"` // RSA modulus (base64url)
	E   string `json:"e,omitempty"` // RSA public exponent
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
}

type jwks struct {
	Keys []jwk `json:"keys"`
}

// StaticKeySet loads a JWKS from disk once at startup.
type StaticKeySet struct {
	keys map[string]any
}

// LoadStatic reads and parses a JWKS file.
func LoadStatic(path string) (*StaticKeySet, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-supplied path
	if err != nil {
		return nil, fmt.Errorf("jwt: read static jwks %q: %w", path, err)
	}
	keys, err := parseJWKS(raw)
	if err != nil {
		return nil, err
	}
	return &StaticKeySet{keys: keys}, nil
}

// LookupKey returns the parsed public key for kid.
func (s *StaticKeySet) LookupKey(_ context.Context, kid string) (any, error) {
	k, ok := s.keys[kid]
	if !ok {
		return nil, fmt.Errorf("jwt: kid %q not found in static jwks", kid)
	}
	return k, nil
}

// RemoteKeySet fetches a JWKS over HTTP and refreshes periodically.
type RemoteKeySet struct {
	url          string
	client       *http.Client
	mu           sync.RWMutex
	keys         map[string]any
	fetched      time.Time
	ttl          time.Duration
	bootstrapErr error
}

// LoadRemote initializes a RemoteKeySet and performs an initial fetch. If the
// initial fetch fails, the error is returned (fail-fast on startup per AGENTS).
func LoadRemote(ctx context.Context, url string, client *http.Client) (*RemoteKeySet, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	r := &RemoteKeySet{url: url, client: client, ttl: 10 * time.Minute}
	if err := r.refresh(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

// LookupKey returns the cached key, refreshing if TTL expired.
func (r *RemoteKeySet) LookupKey(ctx context.Context, kid string) (any, error) {
	r.mu.RLock()
	expired := time.Since(r.fetched) > r.ttl
	k, ok := r.keys[kid]
	r.mu.RUnlock()
	if ok && !expired {
		return k, nil
	}
	if err := r.refresh(ctx); err != nil {
		// On refresh failure, serve from stale cache if we have it.
		r.mu.RLock()
		k2, ok2 := r.keys[kid]
		r.mu.RUnlock()
		if ok2 {
			return k2, nil
		}
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok = r.keys[kid]
	if !ok {
		return nil, fmt.Errorf("jwt: kid %q not found in remote jwks", kid)
	}
	return k, nil
}

func (r *RemoteKeySet) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return fmt.Errorf("jwt: build jwks request: %w", err)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("jwt: fetch jwks %q: %w", r.url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwt: jwks %q HTTP %d", r.url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return fmt.Errorf("jwt: read jwks: %w", err)
	}
	keys, err := parseJWKS(body)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.keys = keys
	r.fetched = time.Now()
	r.mu.Unlock()
	return nil
}

// parseJWKS turns a JSON document into a map of kid -> *rsa.PublicKey or *ecdsa.PublicKey.
// Symmetric (kty=oct) keys are dropped intentionally.
func parseJWKS(raw []byte) (map[string]any, error) {
	var doc jwks
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("jwt: parse jwks: %w", err)
	}
	out := make(map[string]any, len(doc.Keys))
	for i := range doc.Keys {
		k := doc.Keys[i]
		if k.Kid == "" {
			return nil, errors.New("jwt: jwks key missing kid")
		}
		switch k.Kty {
		case "RSA":
			pub, err := parseRSA(k)
			if err != nil {
				return nil, fmt.Errorf("jwt: jwks kid %q: %w", k.Kid, err)
			}
			out[k.Kid] = pub
		case "EC":
			pub, err := parseEC(k)
			if err != nil {
				return nil, fmt.Errorf("jwt: jwks kid %q: %w", k.Kid, err)
			}
			out[k.Kid] = pub
		default:
			// Skip unsupported types (incl. "oct" symmetric).
			continue
		}
	}
	return out, nil
}

func parseRSA(k jwk) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("rsa modulus decode: %w", err)
	}
	eb, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("rsa exponent decode: %w", err)
	}
	n := new(big.Int).SetBytes(nb)
	var e int
	for _, b := range eb {
		e = e<<8 | int(b)
	}
	if e == 0 {
		return nil, errors.New("rsa exponent is zero")
	}
	return &rsa.PublicKey{N: n, E: e}, nil
}

func parseEC(k jwk) (*ecdsa.PublicKey, error) {
	curve, err := ecCurveFor(k.Crv)
	if err != nil {
		return nil, err
	}
	xb, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("ec x decode: %w", err)
	}
	yb, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, fmt.Errorf("ec y decode: %w", err)
	}
	return &ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(xb), Y: new(big.Int).SetBytes(yb)}, nil
}
