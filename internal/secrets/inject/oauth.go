package inject

import (
	"context"
	"errors"
	"fmt"

	"github.com/hurtener/Portico_gateway/internal/secrets/oauth"
)

// oauthInjector wraps an *oauth.Exchanger and writes the exchanged token
// to target.Headers["Authorization"]. The exchanger is per-server (one
// IdP + audience pair); the dispatcher constructs them at boot per
// auth.exchange spec and registers a per-server injector.
type oauthInjector struct {
	exchanger *oauth.Exchanger
}

// NewOAuthInjector builds an oauth2_token_exchange strategy.
func NewOAuthInjector(ex *oauth.Exchanger) Injector { return &oauthInjector{exchanger: ex} }

func (o *oauthInjector) Strategy() string { return StrategyOAuth2Exchange }

func (o *oauthInjector) Apply(ctx context.Context, req PrepRequest, target *PrepTarget) error {
	if o.exchanger == nil {
		return errors.New("inject oauth2_token_exchange: exchanger not configured")
	}
	if req.SubjectToken == "" {
		return oauth.ErrNoSubjectToken
	}
	tok, err := o.exchanger.Exchange(ctx, req.TenantID, req.UserID, req.SubjectToken)
	if err != nil {
		return fmt.Errorf("inject oauth2_token_exchange: %w", err)
	}
	if target.Headers == nil {
		target.Headers = make(map[string]string, 1)
	}
	target.Headers["Authorization"] = "Bearer " + tok.AccessToken
	return nil
}
