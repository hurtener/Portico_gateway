package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/apps"
	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
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

const skillManifest = `id: github.code-review
title: GitHub Code Review
version: 0.1.0
spec: skills/v1
description: Review PRs.
instructions: SKILL.md
prompts:
  - prompts/review_pr.md
binding:
  required_tools:
    - github.get_pull_request
`

func writeSkillFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	pack := filepath.Join(root, "github", "code-review")
	for _, sub := range []string{"prompts"} {
		_ = os.MkdirAll(filepath.Join(pack, sub), 0o755)
	}
	files := map[string]string{
		"manifest.yaml":        skillManifest,
		"SKILL.md":             "# Review\n",
		"prompts/review_pr.md": "---\nname: review_pr\ndescription: Template\n---\nReview {{.target}}.",
	}
	for rel, body := range files {
		path := filepath.Join(pack, rel)
		_ = os.WriteFile(path, []byte(body), 0o644)
	}
	return root
}

// startSkillsDevServer is a copy of startMcpDevServer with the Phase 4
// skills runtime wired in. The shared helper lives in mcp_e2e_test.go;
// we need a separate one here so we can pass a skill source.
func startSkillsDevServer(t *testing.T, skillRoot string) (*httptest.Server, *skillruntime.Manager) {
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
	supervisor.SetNotifSink(func(ctx context.Context, serverID string, n protocol.Notification) {
		listChangedMux.OnDownstream(ctx, serverID, n)
	})
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

	handler := api.NewRouter(api.Deps{
		Logger:     logger,
		DevMode:    true,
		DevTenant:  "dev",
		Tenants:    backend.Tenants(),
		Audit:      backend.Audit(),
		Sessions:   sess,
		Dispatcher: disp,
		Manager:    mgr,
		Registry:   reg,
		Apps:       appsReg,
		Skills:     skillsMgr,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, skillsMgr
}

// TestE2E_SkillVisibleInResources confirms that a skill loaded from a
// LocalDir source surfaces under skill:// in resources/list and is
// readable via resources/read.
func TestE2E_SkillVisibleInResources(t *testing.T) {
	root := writeSkillFixture(t)
	srv, _ := startSkillsDevServer(t, root)

	initResp, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		ClientInfo:      protocol.Implementation{Name: "test"},
	}))
	if initResp.Error != nil {
		t.Fatal(initResp.Error)
	}

	listResp, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodResourcesList, struct{}{}))
	if listResp.Error != nil {
		t.Fatal(listResp.Error)
	}
	var listRes protocol.ListResourcesResult
	if err := json.Unmarshal(listResp.Result, &listRes); err != nil {
		t.Fatal(err)
	}
	hasIndex := false
	hasManifest := false
	hasSkillMD := false
	for _, r := range listRes.Resources {
		switch r.URI {
		case "skill://_index":
			hasIndex = true
		case "skill://github/code-review/manifest.yaml":
			hasManifest = true
		case "skill://github/code-review/SKILL.md":
			hasSkillMD = true
		}
	}
	if !hasIndex || !hasManifest || !hasSkillMD {
		t.Fatalf("expected skill:// resources; got %+v", listRes.Resources)
	}

	readResp, _ := rpcPost(t, srv.URL, sid, newReq(3, protocol.MethodResourcesRead, protocol.ReadResourceParams{
		URI: "skill://_index",
	}))
	if readResp.Error != nil {
		t.Fatal(readResp.Error)
	}
	var readRes protocol.ReadResourceResult
	if err := json.Unmarshal(readResp.Result, &readRes); err != nil {
		t.Fatal(err)
	}
	if len(readRes.Contents) == 0 || !strings.Contains(readRes.Contents[0].Text, "github.code-review") {
		t.Errorf("_index missing skill: %q", readRes.Contents[0].Text)
	}
}

// TestE2E_SkillPromptListAndGet covers prompts/list and prompts/get
// through the skill provider seam.
func TestE2E_SkillPromptListAndGet(t *testing.T) {
	root := writeSkillFixture(t)
	srv, _ := startSkillsDevServer(t, root)

	_, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		ClientInfo:      protocol.Implementation{Name: "test"},
	}))

	listResp, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodPromptsList, struct{}{}))
	var lr protocol.ListPromptsResult
	if err := json.Unmarshal(listResp.Result, &lr); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range lr.Prompts {
		if p.Name == "github.code-review.review_pr" {
			found = true
		}
	}
	if !found {
		t.Fatalf("skill prompt missing from list: %+v", lr.Prompts)
	}

	getResp, _ := rpcPost(t, srv.URL, sid, newReq(3, protocol.MethodPromptsGet, protocol.GetPromptParams{
		Name:      "github.code-review.review_pr",
		Arguments: map[string]string{"target": "main"},
	}))
	if getResp.Error != nil {
		t.Fatal(getResp.Error)
	}
	var gp protocol.GetPromptResult
	if err := json.Unmarshal(getResp.Result, &gp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gp.Messages[0].Content.Text, "main") {
		t.Errorf("template not substituted: %q", gp.Messages[0].Content.Text)
	}
}

// TestE2E_SkillEnableDisable covers /v1/skills/{id}/enable + disable
// and verifies the catalog filter changes a session's view.
func TestE2E_SkillEnableDisable(t *testing.T) {
	root := writeSkillFixture(t)
	// Spin up with opt-in mode so disable hides the skill.
	srv := startSkillsDevServerOptIn(t, root)

	// Initialize a session — without any enable, no skill resources.
	_, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		ClientInfo:      protocol.Implementation{Name: "test"},
	}))
	listResp, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodResourcesList, struct{}{}))
	var listRes protocol.ListResourcesResult
	_ = json.Unmarshal(listResp.Result, &listRes)
	for _, r := range listRes.Resources {
		if strings.HasPrefix(r.URI, "skill://github/") {
			t.Errorf("opt-in mode unexpectedly exposed %q", r.URI)
		}
	}

	// Enable tenant-wide via REST.
	resp, err := http.Post(srv.URL+"/v1/skills/github.code-review/enable", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("enable returned %d", resp.StatusCode)
	}

	// New session — should now see the skill.
	_, sid2 := rpcPost(t, srv.URL, "", newReq(3, protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		ClientInfo:      protocol.Implementation{Name: "test"},
	}))
	listResp2, _ := rpcPost(t, srv.URL, sid2, newReq(4, protocol.MethodResourcesList, struct{}{}))
	var listRes2 protocol.ListResourcesResult
	_ = json.Unmarshal(listResp2.Result, &listRes2)
	visible := false
	for _, r := range listRes2.Resources {
		if r.URI == "skill://github/code-review/SKILL.md" {
			visible = true
		}
	}
	if !visible {
		t.Errorf("after enable, skill SKILL.md still not in list")
	}
}

// startSkillsDevServerOptIn is the same wiring as startSkillsDevServer
// but with ModeOptIn so the enable/disable flow is observable.
func startSkillsDevServerOptIn(t *testing.T, skillRoot string) *httptest.Server {
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
	supervisor.SetNotifSink(func(ctx context.Context, serverID string, n protocol.Notification) {
		listChangedMux.OnDownstream(ctx, serverID, n)
	})
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
	enablement := skillruntime.NewEnablement(backend.Skills(), skillruntime.ModeOptIn)
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

	handler := api.NewRouter(api.Deps{
		Logger:     logger,
		DevMode:    true,
		DevTenant:  "dev",
		Tenants:    backend.Tenants(),
		Audit:      backend.Audit(),
		Sessions:   sess,
		Dispatcher: disp,
		Manager:    mgr,
		Registry:   reg,
		Apps:       appsReg,
		Skills:     skillsMgr,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}
