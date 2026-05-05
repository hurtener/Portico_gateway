// Package jwt validates JWTs against an issuer + audience + JWKS.
//
// Algorithm allowlist: RS256, RS384, RS512, ES256, ES384, ES512.
// Symmetric algorithms (HS*) and "none" are forbidden — see AGENTS.md §7.
package jwt

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"errors"
	"fmt"
	"strings"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"

	"github.com/hurtener/Portico_gateway/internal/config"
)

// Allowed algorithms. Asymmetric only.
var allowedAlgs = []string{
	jwtv5.SigningMethodRS256.Name,
	jwtv5.SigningMethodRS384.Name,
	jwtv5.SigningMethodRS512.Name,
	jwtv5.SigningMethodES256.Name,
	jwtv5.SigningMethodES384.Name,
	jwtv5.SigningMethodES512.Name,
}

// Claims is the validated, normalized claim set Portico cares about.
type Claims struct {
	Subject   string
	Issuer    string
	Audience  []string
	Tenant    string
	Plan      string
	Scopes    []string
	ExpiresAt time.Time
	IssuedAt  time.Time
	Raw       map[string]any
}

// Validator validates a Bearer token.
type Validator struct {
	cfg     config.JWTConfig
	keyset  KeySet
	parser  *jwtv5.Parser
	skew    time.Duration
}

// NewValidator builds a Validator wired to either a static or remote JWKS.
func NewValidator(ctx context.Context, cfg config.JWTConfig) (*Validator, error) {
	var ks KeySet
	switch {
	case cfg.StaticJWKS != "":
		s, err := LoadStatic(cfg.StaticJWKS)
		if err != nil {
			return nil, err
		}
		ks = s
	case cfg.JWKSURL != "":
		r, err := LoadRemote(ctx, cfg.JWKSURL, nil)
		if err != nil {
			return nil, err
		}
		ks = r
	default:
		return nil, errors.New("jwt: either static_jwks or jwks_url is required")
	}

	skew := cfg.ClockSkew
	if skew == 0 {
		skew = 60 * time.Second
	}

	parser := jwtv5.NewParser(
		jwtv5.WithValidMethods(allowedAlgs),
		jwtv5.WithLeeway(skew),
		jwtv5.WithIssuedAt(),
		jwtv5.WithIssuer(cfg.Issuer),
	)

	return &Validator{
		cfg:    cfg,
		keyset: ks,
		parser: parser,
		skew:   skew,
	}, nil
}

// Validate parses, signature-checks, and normalizes a raw bearer token.
func (v *Validator) Validate(ctx context.Context, raw string) (*Claims, error) {
	if raw == "" {
		return nil, errors.New("jwt: empty token")
	}

	tok, err := v.parser.Parse(raw, func(t *jwtv5.Token) (any, error) {
		// Defence in depth: enforce alg allowlist explicitly even though
		// WithValidMethods covers it.
		alg, _ := t.Header["alg"].(string)
		if !algAllowed(alg) {
			return nil, fmt.Errorf("disallowed alg %q", alg)
		}
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("missing kid header")
		}
		k, err := v.keyset.LookupKey(ctx, kid)
		if err != nil {
			return nil, err
		}
		// Sanity: ensure key type matches alg family.
		if err := assertKeyAlg(k, alg); err != nil {
			return nil, err
		}
		return k, nil
	})
	if err != nil {
		return nil, fmt.Errorf("jwt: %w", err)
	}
	if !tok.Valid {
		return nil, errors.New("jwt: invalid token")
	}

	mc, ok := tok.Claims.(jwtv5.MapClaims)
	if !ok {
		return nil, errors.New("jwt: unexpected claims type")
	}

	// Audience check: at least one of cfg.Audiences must appear.
	if len(v.cfg.Audiences) > 0 {
		ok := false
		for _, want := range v.cfg.Audiences {
			audClaim, _ := mc.GetAudience()
			for _, got := range audClaim {
				if got == want {
					ok = true
					break
				}
			}
			if ok {
				break
			}
		}
		if !ok {
			return nil, errors.New("jwt: audience mismatch")
		}
	}

	c := &Claims{Raw: map[string]any(mc)}
	if iss, ok := mc["iss"].(string); ok {
		c.Issuer = iss
	}
	if sub, ok := mc["sub"].(string); ok {
		c.Subject = sub
	}
	if aud, err := mc.GetAudience(); err == nil {
		c.Audience = aud
	}
	if exp, err := mc.GetExpirationTime(); err == nil && exp != nil {
		c.ExpiresAt = exp.Time
	}
	if iat, err := mc.GetIssuedAt(); err == nil && iat != nil {
		c.IssuedAt = iat.Time
	}
	tenantClaim := v.cfg.TenantClaim
	if tenantClaim == "" {
		tenantClaim = "tenant"
	}
	if tenant, ok := mc[tenantClaim].(string); ok {
		c.Tenant = tenant
	}
	if c.Tenant == "" {
		return nil, fmt.Errorf("jwt: tenant claim %q missing", tenantClaim)
	}
	if plan, ok := mc["plan"].(string); ok {
		c.Plan = plan
	}
	scopeClaim := v.cfg.ScopeClaim
	if scopeClaim == "" {
		scopeClaim = "scope"
	}
	switch sv := mc[scopeClaim].(type) {
	case string:
		c.Scopes = strings.Fields(sv)
	case []any:
		for _, s := range sv {
			if str, ok := s.(string); ok {
				c.Scopes = append(c.Scopes, str)
			}
		}
	}

	if v.cfg.RequiredScope != "" {
		ok := false
		for _, s := range c.Scopes {
			if s == v.cfg.RequiredScope {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("jwt: missing required scope %q", v.cfg.RequiredScope)
		}
	}

	return c, nil
}

func algAllowed(alg string) bool {
	for _, a := range allowedAlgs {
		if a == alg {
			return true
		}
	}
	return false
}

func assertKeyAlg(key any, alg string) error {
	switch alg {
	case "RS256", "RS384", "RS512":
		if _, ok := key.(*rsaKeyMarker); ok {
			return nil
		}
		// Real *rsa.PublicKey — verified at signing-method level by jwt/v5
		return nil
	case "ES256", "ES384", "ES512":
		if pk, ok := key.(*ecdsa.PublicKey); ok {
			if !curveMatches(pk.Curve, alg) {
				return fmt.Errorf("alg %s does not match curve", alg)
			}
		}
	}
	return nil
}

// rsaKeyMarker exists to keep the type assertion above syntactically valid in
// case someone refactors. Real RSA keys are *rsa.PublicKey.
type rsaKeyMarker struct{}

func curveMatches(c elliptic.Curve, alg string) bool {
	switch alg {
	case "ES256":
		return c.Params().BitSize == 256
	case "ES384":
		return c.Params().BitSize == 384
	case "ES512":
		return c.Params().BitSize == 521
	}
	return false
}

// ecCurveFor maps a JWK crv name to a Go curve.
func ecCurveFor(crv string) (elliptic.Curve, error) {
	switch crv {
	case "P-256":
		return elliptic.P256(), nil
	case "P-384":
		return elliptic.P384(), nil
	case "P-521":
		return elliptic.P521(), nil
	default:
		return nil, fmt.Errorf("unsupported ec curve %q", crv)
	}
}
