package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/profiles"
)

func withProfile(r *http.Request, prof *profiles.Profile) *http.Request {
	return r.WithContext(profiles.WithProfile(r.Context(), prof))
}

func TestAliasAllowedByProfile(t *testing.T) {
	restrictive := &profiles.Profile{ID: "ap_1", AllowedModelAliases: []string{"gpt-4o"}}

	// In-surface alias → allowed, nothing written.
	r := withProfile(newReq("POST", "/v1/chat/completions", nil), restrictive)
	w := httptest.NewRecorder()
	if !aliasAllowedByProfile(w, r, "gpt-4o") {
		t.Fatal("in-surface alias rejected")
	}

	// Out-of-surface → 403 agent_profile_violation.
	w = httptest.NewRecorder()
	if aliasAllowedByProfile(w, r, "claude-3-5-sonnet") {
		t.Fatal("out-of-surface alias allowed")
	}
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "agent_profile_violation") {
		t.Fatalf("expected 403 agent_profile_violation, got %d %s", w.Code, w.Body.String())
	}

	// Default + absent profile → allowed.
	rd := withProfile(newReq("POST", "/x", nil), profiles.DefaultProfile("t1"))
	if !aliasAllowedByProfile(httptest.NewRecorder(), rd, "anything") {
		t.Error("default profile must allow any alias")
	}
	if !aliasAllowedByProfile(httptest.NewRecorder(), newReq("POST", "/x", nil), "anything") {
		t.Error("absent profile must allow any alias")
	}
}

func TestFilterAliasesByProfile(t *testing.T) {
	type m struct{ alias string }
	items := []m{{"a"}, {"b"}, {"c"}}
	aliasOf := func(x m) string { return x.alias }

	r := withProfile(newReq("GET", "/v1/models", nil), &profiles.Profile{AllowedModelAliases: []string{"b"}})
	got := filterAliasesByProfile(r, items, aliasOf)
	if len(got) != 1 || got[0].alias != "b" {
		t.Fatalf("filter wrong: %+v", got)
	}

	rd := withProfile(newReq("GET", "/v1/models", nil), profiles.DefaultProfile("t1"))
	if len(filterAliasesByProfile(rd, items, aliasOf)) != 3 {
		t.Error("default profile must not filter aliases")
	}
}

func TestListModels_FilteredByProfile(t *testing.T) {
	d, _ := llmDepsWithEmbedding()

	// Discover the seeded aliases via an unrestricted list.
	base := runHandler(listModelsHandler(d), newReq("GET", "/v1/models", nil, ScopeLLMInvoke))
	var all openAIModelsResponse
	decodeJSON(t, base, &all)
	if len(all.Data) < 2 {
		t.Skipf("need >=2 seeded models, got %d", len(all.Data))
	}
	allowed := all.Data[0].ID

	// With a profile allowing only the first alias, the list narrows to it.
	r := withProfile(newReq("GET", "/v1/models", nil, ScopeLLMInvoke),
		&profiles.Profile{ID: "ap_1", AllowedModelAliases: []string{allowed}})
	w := runHandler(listModelsHandler(d), r)
	var resp openAIModelsResponse
	decodeJSON(t, w, &resp)
	if len(resp.Data) != 1 || resp.Data[0].ID != allowed {
		t.Fatalf("profile-filtered /v1/models wrong: %+v", resp.Data)
	}
}
