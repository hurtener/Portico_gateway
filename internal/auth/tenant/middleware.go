package tenant

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hurtener/Portico_gateway/internal/auth/jwt"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// pathsAlwaysAllowed bypass auth (health, console assets).
var pathsAlwaysAllowed = []string{
	"/healthz",
	"/readyz",
	"/favicon.svg",
	"/favicon.ico",
	"/robots.txt",
}

// assetPrefixesAlwaysAllowed lists path prefixes that bypass auth — these
// are static assets the Console SPA needs even before the user has a
// session (the SvelteKit runtime is loaded from `/_app/`).
var assetPrefixesAlwaysAllowed = []string{
	"/_app/",
}

func isAlwaysAllowed(path string) bool {
	for _, p := range pathsAlwaysAllowed {
		if path == p {
			return true
		}
	}
	for _, p := range assetPrefixesAlwaysAllowed {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// MiddlewareConfig captures everything Middleware needs.
type MiddlewareConfig struct {
	Validator   *jwt.Validator // may be nil in dev mode
	DevMode     bool
	DevTenant   string // tenant id to inject in dev mode
	TenantStore ifaces.TenantStore
	Logger      *slog.Logger
}

// Middleware authenticates incoming requests.
//
//   - Dev mode: skip JWT entirely; inject the synthetic dev identity. The dev
//     tenant is upserted on first request.
//   - Production: require Authorization: Bearer <jwt>; validate; look up the
//     tenant in the store; reject if unknown.
func Middleware(cfg MiddlewareConfig) func(http.Handler) http.Handler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.DevMode && cfg.DevTenant == "" {
		cfg.DevTenant = devTenantFromEnv()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isAlwaysAllowed(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			if cfg.DevMode {
				if err := ensureDevTenant(r.Context(), cfg.TenantStore, cfg.DevTenant); err != nil {
					cfg.Logger.Warn("failed to upsert dev tenant", "err", err, "tenant", cfg.DevTenant)
				}
				id := Identity{
					TenantID: cfg.DevTenant,
					UserID:   "dev",
					Plan:     "enterprise",
					Scopes:   []string{"admin"},
					DevMode:  true,
				}
				next.ServeHTTP(w, r.WithContext(With(r.Context(), id)))
				return
			}

			if cfg.Validator == nil {
				writeJSON(w, http.StatusInternalServerError,
					errorBody{Error: "auth_misconfigured", Message: "no JWT validator and not in dev mode"})
				return
			}

			authz := r.Header.Get("Authorization")
			if !strings.HasPrefix(authz, "Bearer ") {
				w.Header().Set("WWW-Authenticate", `Bearer realm="portico"`)
				writeJSON(w, http.StatusUnauthorized,
					errorBody{Error: "unauthorized", Message: "missing bearer token"})
				return
			}
			raw := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
			claims, err := cfg.Validator.Validate(r.Context(), raw)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer realm="portico"`)
				writeJSON(w, http.StatusUnauthorized,
					errorBody{Error: "unauthorized", Message: err.Error()})
				return
			}

			t, err := cfg.TenantStore.Get(r.Context(), claims.Tenant)
			if err != nil || t == nil {
				w.Header().Set("WWW-Authenticate", `Bearer realm="portico"`)
				writeJSON(w, http.StatusUnauthorized,
					errorBody{Error: "unknown_tenant", Message: "tenant not registered"})
				return
			}

			id := Identity{
				TenantID: t.ID,
				UserID:   claims.Subject,
				Plan:     firstNonEmpty(claims.Plan, t.Plan),
				Scopes:   claims.Scopes,
				Issuer:   claims.Issuer,
				Subject:  claims.Subject,
			}
			next.ServeHTTP(w, r.WithContext(With(r.Context(), id)))
		})
	}
}

// ensureDevTenant upserts a synthetic dev tenant if it doesn't exist yet.
// Idempotent and cheap.
func ensureDevTenant(ctx context.Context, store ifaces.TenantStore, id string) error {
	if store == nil || id == "" {
		return nil
	}
	t, err := store.Get(ctx, id)
	if err == nil && t != nil {
		return nil
	}
	return store.Upsert(ctx, &ifaces.Tenant{
		ID:          id,
		DisplayName: "Development Tenant",
		Plan:        "enterprise",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	})
}

func devTenantFromEnv() string {
	if v := os.Getenv("PORTICO_DEV_TENANT"); v != "" {
		return v
	}
	return "dev"
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
