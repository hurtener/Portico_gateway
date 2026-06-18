package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/auth/tenant"
	"github.com/hurtener/Portico_gateway/internal/profiles"
)

type fakeResolver struct {
	profile *profiles.Profile
	err     error
	calls   int
}

func (f *fakeResolver) Resolve(_ context.Context, _ profiles.Principal) (*profiles.Profile, error) {
	f.calls++
	return f.profile, f.err
}
func (f *fakeResolver) Invalidate(_, _ string) {}

func TestProfileMiddleware_NoIdentity_PassesThrough(t *testing.T) {
	res := &fakeResolver{}
	var sawProfile bool
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		sawProfile = profiles.FromContext(r.Context()) != nil
	})
	rec := httptest.NewRecorder()
	ProfileMiddleware(res, nil)(next).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if res.calls != 0 || sawProfile {
		t.Fatalf("no-identity request must pass through without resolving: calls=%d sawProfile=%v", res.calls, sawProfile)
	}
}

func TestProfileMiddleware_SetsProfileInContext(t *testing.T) {
	want := &profiles.Profile{TenantID: "t1", ID: "ap_1", AllowedMCPServers: []string{"github"}}
	res := &fakeResolver{profile: want}
	var got *profiles.Profile
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = profiles.FromContext(r.Context())
	})
	r := httptest.NewRequest("GET", "/", nil).WithContext(
		tenant.With(context.Background(), tenant.Identity{TenantID: "t1", Subject: "sub-1"}))
	rec := httptest.NewRecorder()
	ProfileMiddleware(res, nil)(next).ServeHTTP(rec, r)
	if got == nil || got.ID != "ap_1" {
		t.Fatalf("middleware did not write the resolved profile into context: %+v", got)
	}
}

func TestProfileMiddleware_ResolverError_FailsClosed503(t *testing.T) {
	res := &fakeResolver{err: errors.New("db down")}
	called := false
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { called = true })
	r := httptest.NewRequest("GET", "/", nil).WithContext(
		tenant.With(context.Background(), tenant.Identity{TenantID: "t1", Subject: "sub-1"}))
	rec := httptest.NewRecorder()
	ProfileMiddleware(res, nil)(next).ServeHTTP(rec, r)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("resolver error must fail closed with 503, got %d", rec.Code)
	}
	if called {
		t.Fatal("downstream handler must not run when entitlement is unknown")
	}
}
