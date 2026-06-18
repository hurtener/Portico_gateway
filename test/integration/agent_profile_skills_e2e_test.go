package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/apps"
	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/profiles"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/runtime/process"
	"github.com/hurtener/Portico_gateway/internal/server/api"
	"github.com/hurtener/Portico_gateway/internal/server/mcpgw"
	skillloader "github.com/hurtener/Portico_gateway/internal/skills/loader"
	skillruntime "github.com/hurtener/Portico_gateway/internal/skills/runtime"
	skillsource "github.com/hurtener/Portico_gateway/internal/skills/source"
	"github.com/hurtener/Portico_gateway/internal/storage"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
	_ "github.com/hurtener/Portico_gateway/internal/storage/sqlite"
)

// startSkillsProfileDevServer mirrors startSkillsDevServer (auto-enable skills
// from a LocalDir source) but additionally wires the Phase 14 agent-profile
// resolver + middleware, so a bound profile gates the skills surface.
func startSkillsProfileDevServer(t *testing.T, skillRoot string) (*httptest.Server, profiles.Resolver) {
	t.Helper()
	cfg := &config.Config{
		Server:  config.ServerConfig{Bind: "127.0.0.1:0"},
		Storage: config.StorageConfig{Driver: "sqlite", DSN: ":memory:"},
		Logging: config.LoggingConfig{Level: "error", Format: "json"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	backend, err := storage.Open(context.Background(), cfg.Storage, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })

	if err := backend.Tenants().Upsert(context.Background(), &ifaces.Tenant{
		ID: "dev", DisplayName: "dev", Plan: "free",
	}); err != nil {
		t.Fatal(err)
	}

	reg := registry.New(backend.Registry(), logger)
	supervisor := process.NewSupervisor(logger, process.NewResolver(nil), reg)
	mgr := southboundmgr.NewManager(reg, supervisor, logger)
	disp := mcpgw.NewDispatcher(mgr, logger)
	sess := mcpgw.NewSessionRegistry()

	appsReg := apps.New(apps.DefaultCSP())
	resourceAgg := mcpgw.NewResourceAggregator(mgr, appsReg, mcpgw.DefaultResourceLimits(), logger)
	promptAgg := mcpgw.NewPromptAggregator(mgr, resourceAgg, logger)
	listChangedMux := mcpgw.NewListChangedMux(sess, resourceAgg, mcpgw.ModeStable, logger)
	disp.SetAggregators(resourceAgg, promptAgg, listChangedMux)
	sess.OnClose(listChangedMux.ForgetSession)
	sess.OnClose(disp.InvalidateSession)

	src, err := skillsource.NewLocalDir(skillRoot, logger)
	if err != nil {
		t.Fatal(err)
	}
	loaderInst, err := skillloader.New([]skillsource.Source{src}, reg, logger)
	if err != nil {
		t.Fatal(err)
	}
	enablement := skillruntime.NewEnablement(backend.Skills(), skillruntime.ModeAuto)
	skillsMgr := skillruntime.NewManager(loaderInst, enablement, nil, nil, logger)
	if err := skillsMgr.Start(context.Background(), []skillsource.Source{src}); err != nil {
		t.Fatal(err)
	}
	resourceAgg.SetSkillProvider(skillsMgr.Provider(), nil)

	t.Cleanup(func() {
		skillsMgr.Stop()
		sess.CloseAll()
		_ = mgr.CloseAll(context.Background())
		reg.CloseAll()
	})

	resolver := profiles.NewResolver(backend.AgentProfiles(), 0, 0)
	handler := api.NewRouter(api.Deps{
		Logger:          logger,
		DevMode:         true,
		DevTenant:       "dev",
		Tenants:         backend.Tenants(),
		Audit:           backend.Audit(),
		Sessions:        sess,
		Dispatcher:      disp,
		Manager:         mgr,
		Registry:        reg,
		Apps:            appsReg,
		Skills:          skillsMgr,
		AgentProfiles:   backend.AgentProfiles(),
		ProfileResolver: resolver,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, resolver
}

// TestE2E_AgentProfile_SkillsFiltered is the acceptance-#8 proof: a bound
// profile that does not allow github.code-review hides the skill's prompts and
// skill:// resources, denies a direct read/get with the typed violation, and
// prunes it from the skill://_index body.
func TestE2E_AgentProfile_SkillsFiltered(t *testing.T) {
	root := writeSkillFixture(t)
	srv, _ := startSkillsProfileDevServer(t, root)

	// Baseline (no profile bound): the skill is visible.
	_, baseSid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		ClientInfo:      protocol.Implementation{Name: "base"},
	}))
	basePrompts, _ := rpcPost(t, srv.URL, baseSid, newReq(2, protocol.MethodPromptsList, struct{}{}))
	var bp protocol.ListPromptsResult
	_ = json.Unmarshal(basePrompts.Result, &bp)
	if !promptPresent(bp.Prompts, "github.code-review.review_pr") {
		t.Fatalf("baseline prompts/list missing the skill prompt: %+v", bp.Prompts)
	}

	// Bind dev to a profile that allows a DIFFERENT skill (empty scopes → no
	// scope narrowing, so admin REST stays usable).
	var created struct {
		ID string `json:"id"`
	}
	restJSON(t, srv, "POST", "/api/agent-profiles",
		`{"name":"no-skills","allowed_mcp_servers":[],"allowed_tools":[],"allowed_skills":["other.skill"],"allowed_model_aliases":[],"scopes":[],"enabled":true}`,
		http.StatusCreated, &created)
	restJSON(t, srv, "PUT", "/api/agent-profiles/"+created.ID+"/bindings/dev", "", http.StatusNoContent, nil)

	// A fresh session resolves dev → the restricted profile.
	_, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		ClientInfo:      protocol.Implementation{Name: "restricted"},
	}))

	// prompts/list omits the skill prompt.
	pl, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodPromptsList, struct{}{}))
	var lp protocol.ListPromptsResult
	_ = json.Unmarshal(pl.Result, &lp)
	if promptPresent(lp.Prompts, "github.code-review.review_pr") {
		t.Fatalf("skill prompt not filtered by the bound profile: %+v", lp.Prompts)
	}

	// resources/list omits the skill resource but retains _index.
	rl, _ := rpcPost(t, srv.URL, sid, newReq(3, protocol.MethodResourcesList, struct{}{}))
	var lr protocol.ListResourcesResult
	_ = json.Unmarshal(rl.Result, &lr)
	var sawIndex bool
	for _, r := range lr.Resources {
		if r.URI == "skill://_index" {
			sawIndex = true
		}
		if strings.HasPrefix(r.URI, "skill://github/code-review/") {
			t.Fatalf("skill resource not filtered: %s", r.URI)
		}
	}
	if !sawIndex {
		t.Error("skill://_index must remain visible")
	}

	// resources/read of the disallowed skill → typed violation.
	rr, _ := rpcPost(t, srv.URL, sid, newReq(4, protocol.MethodResourcesRead, protocol.ReadResourceParams{
		URI: "skill://github/code-review/manifest.yaml",
	}))
	if rr.Error == nil || rr.Error.Code != protocol.ErrAgentProfileViolation {
		t.Fatalf("resources/read outside profile not rejected with violation: %+v", rr.Error)
	}

	// prompts/get of the disallowed skill → typed violation.
	pg, _ := rpcPost(t, srv.URL, sid, newReq(5, protocol.MethodPromptsGet, protocol.GetPromptParams{
		Name:      "github.code-review.review_pr",
		Arguments: map[string]string{"target": "main"},
	}))
	if pg.Error == nil || pg.Error.Code != protocol.ErrAgentProfileViolation {
		t.Fatalf("prompts/get outside profile not rejected with violation: %+v", pg.Error)
	}

	// skill://_index body is pruned to the allowed skills (github.code-review gone).
	idx, _ := rpcPost(t, srv.URL, sid, newReq(6, protocol.MethodResourcesRead, protocol.ReadResourceParams{
		URI: "skill://_index",
	}))
	if idx.Error != nil {
		t.Fatalf("_index read errored: %+v", idx.Error)
	}
	var idxRes protocol.ReadResourceResult
	_ = json.Unmarshal(idx.Result, &idxRes)
	if len(idxRes.Contents) == 0 || strings.Contains(idxRes.Contents[0].Text, "github.code-review") {
		t.Fatalf("_index body not pruned to allowed skills: %q", idxRes.Contents[0].Text)
	}
}

// TestE2E_AgentProfile_ScopeIntersection is the acceptance-#11 proof end-to-end:
// a bound profile whose scope set excludes admin narrows the dev principal's
// effective scopes (intersection), so an admin-scoped REST route is rejected
// while the (unscoped) MCP session stays reachable. The profile narrows, never
// broadens. The /v1 alias-rejection half of #11 is covered by the LLM
// enforcement tests; here /v1 is not mounted (no engine wired).
func TestE2E_AgentProfile_ScopeIntersection(t *testing.T) {
	root := writeSkillFixture(t)
	srv, _ := startSkillsProfileDevServer(t, root)

	// Sanity: with no profile, the admin-scoped create route is reachable.
	var created struct {
		ID string `json:"id"`
	}
	restJSON(t, srv, "POST", "/api/agent-profiles",
		`{"name":"mcp-only","allowed_mcp_servers":[],"allowed_tools":[],"allowed_skills":[],"allowed_model_aliases":[],"scopes":["mcp:call"],"enabled":true}`,
		http.StatusCreated, &created)
	// Bind dev to the mcp:call-only profile. dev mode carries [admin]; the
	// intersection [admin] ∩ [mcp:call] = [] strips the admin umbrella.
	restJSON(t, srv, "PUT", "/api/agent-profiles/"+created.ID+"/bindings/dev", "", http.StatusNoContent, nil)

	// /mcp is still reachable (the MCP session does not gate on the admin scope).
	initResp, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		ClientInfo:      protocol.Implementation{Name: "mcp"},
	}))
	if initResp.Error != nil {
		t.Fatalf("/mcp must stay reachable with mcp:call: %+v", initResp.Error)
	}
	if listResp, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodPromptsList, struct{}{})); listResp.Error != nil {
		t.Fatalf("/mcp prompts/list errored after narrowing: %+v", listResp.Error)
	}

	// The admin-scoped REST surface is now forbidden — admin was narrowed away.
	req, _ := http.NewRequest("POST", srv.URL+"/api/agent-profiles",
		strings.NewReader(`{"name":"should-fail","enabled":true}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin REST route must be 403 after scope narrowing, got %d (%s)", resp.StatusCode, b)
	}
}

func promptPresent(ps []protocol.Prompt, name string) bool {
	for _, p := range ps {
		if p.Name == name {
			return true
		}
	}
	return false
}
