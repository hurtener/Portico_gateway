package profiles

import "context"

type ctxKey struct{}

// WithProfile returns a context carrying the resolved profile. The profile
// middleware sets it once per request; downstream gating surfaces read it via
// FromContext.
func WithProfile(ctx context.Context, p *Profile) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext returns the resolved profile, or nil when no middleware ran for
// this request. A nil profile is treated as the default (allow-all) by every
// Profile.Allows* method, so callers can use the result directly without a nil
// check — restriction only ever applies when a real profile is present.
func FromContext(ctx context.Context) *Profile {
	if p, ok := ctx.Value(ctxKey{}).(*Profile); ok {
		return p
	}
	return nil
}
