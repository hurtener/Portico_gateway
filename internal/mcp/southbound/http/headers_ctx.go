package http

import "context"

// headersCtxKey is the context-scope key used by the supervisor and the
// HTTP southbound client to carry per-call auth headers without baking
// them into the cached client config (those would not refresh when an
// OAuth token expires or a tenant rotates a vault entry).
type headersCtxKey struct{}

// WithHeaders attaches per-call auth headers to ctx. The HTTP southbound
// client's HeaderProvider reads them in DefaultHeaderProvider.
func WithHeaders(ctx context.Context, headers map[string]string) context.Context {
	if len(headers) == 0 {
		return ctx
	}
	cp := make(map[string]string, len(headers))
	for k, v := range headers {
		cp[k] = v
	}
	return context.WithValue(ctx, headersCtxKey{}, cp)
}

// HeadersFrom returns the auth headers stored in ctx (or nil).
func HeadersFrom(ctx context.Context) map[string]string {
	v, ok := ctx.Value(headersCtxKey{}).(map[string]string)
	if !ok {
		return nil
	}
	return v
}

// DefaultHeaderProvider is a Config.HeaderProvider that pulls headers
// from the request ctx via WithHeaders. The supervisor wires this for
// every HTTP southbound client so per-call credentials threaded by the
// dispatcher are honored without re-instantiating the client.
func DefaultHeaderProvider(ctx context.Context) (map[string]string, error) {
	return HeadersFrom(ctx), nil
}
