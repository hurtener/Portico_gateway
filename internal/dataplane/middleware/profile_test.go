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

func TestProfileMiddleware_NonDefaultProfile_NarrowsScopes(t *testing.T) {
	// Profile carries [mcp:call]; JWT carries [mcp:call, llm:invoke].
	// Effective = intersection = [mcp:call] (acceptance #11).
	prof := &profiles.Profile{TenantID: "t1", ID: "ap_1", Scopes: []string{"mcp:call"}}
	res := &fakeResolver{profile: prof}
	var gotScopes []string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		id, _ := tenant.From(r.Context())
		gotScopes = id.Scopes
	})
	r := httptest.NewRequest("GET", "/", nil).WithContext(
		tenant.With(context.Background(), tenant.Identity{
			TenantID: "t1", Subject: "sub-1", Scopes: []string{"mcp:call", "llm:invoke"},
		}))
	ProfileMiddleware(res, nil)(next).ServeHTTP(httptest.NewRecorder(), r)
	if len(gotScopes) != 1 || gotScopes[0] != "mcp:call" {
		t.Fatalf("scopes not narrowed to the intersection: %v", gotScopes)
	}
}

func TestProfileMiddleware_ProfileNeverBroadensScopes(t *testing.T) {
	// Profile lists a scope the JWT never carried — it must NOT be granted.
	prof := &profiles.Profile{TenantID: "t1", ID: "ap_1", Scopes: []string{"mcp:call", "admin"}}
	res := &fakeResolver{profile: prof}
	var gotScopes []string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		id, _ := tenant.From(r.Context())
		gotScopes = id.Scopes
	})
	r := httptest.NewRequest("GET", "/", nil).WithContext(
		tenant.With(context.Background(), tenant.Identity{
			TenantID: "t1", Subject: "sub-1", Scopes: []string{"mcp:call"},
		}))
	ProfileMiddleware(res, nil)(next).ServeHTTP(httptest.NewRecorder(), r)
	if len(gotScopes) != 1 || gotScopes[0] != "mcp:call" {
		t.Fatalf("profile must narrow, never broaden: %v", gotScopes)
	}
}

func TestProfileMiddleware_EmptyProfileScopes_PassThrough(t *testing.T) {
	// A profile with no scope set does not constrain scopes.
	prof := &profiles.Profile{TenantID: "t1", ID: "ap_1"}
	res := &fakeResolver{profile: prof}
	var gotScopes []string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		id, _ := tenant.From(r.Context())
		gotScopes = id.Scopes
	})
	r := httptest.NewRequest("GET", "/", nil).WithContext(
		tenant.With(context.Background(), tenant.Identity{
			TenantID: "t1", Subject: "sub-1", Scopes: []string{"mcp:call", "llm:invoke"},
		}))
	ProfileMiddleware(res, nil)(next).ServeHTTP(httptest.NewRecorder(), r)
	if len(gotScopes) != 2 {
		t.Fatalf("empty profile.Scopes must not constrain the JWT scopes: %v", gotScopes)
	}
}

func TestProfileMiddleware_DefaultProfile_DoesNotTouchScopes(t *testing.T) {
	res := &fakeResolver{profile: profiles.DefaultProfile("t1")}
	var gotScopes []string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		id, _ := tenant.From(r.Context())
		gotScopes = id.Scopes
	})
	r := httptest.NewRequest("GET", "/", nil).WithContext(
		tenant.With(context.Background(), tenant.Identity{
			TenantID: "t1", Subject: "sub-1", Scopes: []string{"mcp:call", "llm:invoke"},
		}))
	ProfileMiddleware(res, nil)(next).ServeHTTP(httptest.NewRecorder(), r)
	if len(gotScopes) != 2 {
		t.Fatalf("default profile must leave scopes untouched (back-compat): %v", gotScopes)
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
