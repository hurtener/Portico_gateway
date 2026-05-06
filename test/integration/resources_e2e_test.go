package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/config"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// TestE2E_ResourcesAggregateAndRead_StdioDownstreams covers the
// Phase 3 happy path against two mock servers — one with ui://, one
// with plain resources. The gateway must:
//   - aggregate both lists with namespaced URIs;
//   - route resources/read back to the right downstream;
//   - wrap ui:// HTML with a Content-Security-Policy meta tag;
//   - emit `_meta.upstreamURI` so clients can recover the original URI.
func TestE2E_ResourcesAggregateAndRead_StdioDownstreams(t *testing.T) {
	bin := e2eMockBinary(t)
	srv, _ := startMcpDevServer(t, []config.ServerSpec{
		{ID: "github", Transport: "stdio", Stdio: &config.StdioSpec{Command: bin, Args: []string{"--name", "github", "--resources", "--prompts"}}},
		{ID: "docs", Transport: "stdio", Stdio: &config.StdioSpec{Command: bin, Args: []string{"--name", "docs", "--resources"}}},
	})

	// Initialize a client session.
	initResp, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo:      protocol.Implementation{Name: "test", Version: "0"},
	}))
	if initResp.Error != nil {
		t.Fatalf("initialize: %+v", initResp.Error)
	}
	if sid == "" {
		t.Fatalf("initialize did not allocate a session id")
	}

	// resources/list should aggregate from both servers.
	listResp, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodResourcesList, struct{}{}))
	if listResp.Error != nil {
		t.Fatalf("resources/list: %+v", listResp.Error)
	}
	var listRes protocol.ListResourcesResult
	if err := json.Unmarshal(listResp.Result, &listRes); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	// Each server adds 2 resources: 4 total. URIs must all be namespaced.
	if len(listRes.Resources) != 4 {
		t.Fatalf("expected 4 aggregated resources; got %d (%v)", len(listRes.Resources), listRes.Resources)
	}
	prefixes := map[string]int{}
	for _, r := range listRes.Resources {
		switch {
		case strings.HasPrefix(r.URI, "mcp+server://github/"):
			prefixes["github"]++
		case strings.HasPrefix(r.URI, "mcp+server://docs/"):
			prefixes["docs"]++
		case strings.HasPrefix(r.URI, "ui://github/"):
			prefixes["ui-github"]++
		case strings.HasPrefix(r.URI, "ui://docs/"):
			prefixes["ui-docs"]++
		default:
			t.Errorf("non-namespaced URI in list: %q", r.URI)
		}
	}
	if prefixes["github"] == 0 || prefixes["docs"] == 0 || prefixes["ui-github"] == 0 {
		t.Errorf("aggregation skipped a server: %+v", prefixes)
	}

	// resources/read on a non-ui resource: routes back to the right server,
	// returns the canned body.
	readResp, _ := rpcPost(t, srv.URL, sid, newReq(3, protocol.MethodResourcesRead, protocol.ReadResourceParams{
		URI: "mcp+server://github/file/doc/readme.md",
	}))
	if readResp.Error != nil {
		t.Fatalf("resources/read non-ui: %+v", readResp.Error)
	}
	var readRes protocol.ReadResourceResult
	if err := json.Unmarshal(readResp.Result, &readRes); err != nil {
		t.Fatalf("decode read: %v", err)
	}
	if len(readRes.Contents) != 1 {
		t.Fatalf("expected 1 content chunk, got %d", len(readRes.Contents))
	}
	if !strings.Contains(readRes.Contents[0].Text, "mock content for") {
		t.Errorf("unexpected body: %q", readRes.Contents[0].Text)
	}

	// resources/read on a ui:// resource: gateway wraps with CSP meta.
	uiResp, _ := rpcPost(t, srv.URL, sid, newReq(4, protocol.MethodResourcesRead, protocol.ReadResourceParams{
		URI: "ui://github/panel.html",
	}))
	if uiResp.Error != nil {
		t.Fatalf("resources/read ui: %+v", uiResp.Error)
	}
	var uiRes protocol.ReadResourceResult
	if err := json.Unmarshal(uiResp.Result, &uiRes); err != nil {
		t.Fatalf("decode ui read: %v", err)
	}
	if len(uiRes.Contents) == 0 {
		t.Fatalf("ui read returned no content")
	}
	body := uiRes.Contents[0].Text
	if !strings.Contains(body, "Content-Security-Policy") {
		t.Errorf("ui:// body missing CSP meta: %q", body)
	}
	// _meta.portico.csp + sandbox should be set too.
	var meta map[string]any
	if err := json.Unmarshal(uiRes.Contents[0].Meta, &meta); err != nil {
		t.Fatalf("ui meta is not JSON: %v", err)
	}
	portico, _ := meta["portico"].(map[string]any)
	if portico == nil || portico["csp"] == nil || portico["sandbox"] == nil {
		t.Errorf("ui meta missing csp/sandbox: %+v", meta)
	}
}

// TestE2E_PromptsAggregateAndGet covers the Phase 3 prompt surface.
func TestE2E_PromptsAggregateAndGet(t *testing.T) {
	bin := e2eMockBinary(t)
	srv, _ := startMcpDevServer(t, []config.ServerSpec{
		{ID: "github", Transport: "stdio", Stdio: &config.StdioSpec{Command: bin, Args: []string{"--name", "github", "--prompts"}}},
	})

	initResp, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		ClientInfo:      protocol.Implementation{Name: "test"},
	}))
	if initResp.Error != nil {
		t.Fatal(initResp.Error)
	}
	listResp, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodPromptsList, struct{}{}))
	if listResp.Error != nil {
		t.Fatal(listResp.Error)
	}
	var lr protocol.ListPromptsResult
	if err := json.Unmarshal(listResp.Result, &lr); err != nil {
		t.Fatal(err)
	}
	if len(lr.Prompts) != 2 {
		t.Fatalf("prompts = %d; want 2", len(lr.Prompts))
	}
	for _, p := range lr.Prompts {
		if !strings.HasPrefix(p.Name, "github.") {
			t.Errorf("prompt name not namespaced: %q", p.Name)
		}
	}

	getResp, _ := rpcPost(t, srv.URL, sid, newReq(3, protocol.MethodPromptsGet, protocol.GetPromptParams{
		Name: "github.summarize",
	}))
	if getResp.Error != nil {
		t.Fatal(getResp.Error)
	}
	var gp protocol.GetPromptResult
	if err := json.Unmarshal(getResp.Result, &gp); err != nil {
		t.Fatal(err)
	}
	if gp.Description != "rendered summarize" {
		t.Errorf("descr = %q", gp.Description)
	}

	// Bare names must be rejected.
	bare, _ := rpcPost(t, srv.URL, sid, newReq(4, protocol.MethodPromptsGet, protocol.GetPromptParams{
		Name: "summarize",
	}))
	if bare.Error == nil {
		t.Errorf("expected rejection for bare prompt name")
	}
}
