package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// restJSON issues a REST call against the test server (dev mode → admin scope),
// asserts the status, and decodes the body into out when non-nil.
func restJSON(t *testing.T, srv *httptest.Server, method, path, body string, wantStatus int, out any) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewReader([]byte(body))
	}
	req, _ := http.NewRequest(method, srv.URL+path, rdr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: status %d, want %d (%s)", method, path, resp.StatusCode, wantStatus, b)
	}
	if out != nil {
		_ = json.Unmarshal(b, out)
	}
}

// TestE2E_AgentProfile_FiltersToolsAndRejectsCall is the acceptance-#4/#5 proof
// with a real binding: create a profile that does NOT allow the mock server, bind
// the dev principal to it via REST, then confirm a plain MCP session's tools/list
// omits the mock server's tools and a tools/call is rejected with the typed
// agent_profile_violation.
func TestE2E_AgentProfile_FiltersToolsAndRejectsCall(t *testing.T) {
	srv, _ := startCodeModeServer(t, mockSpec(t))

	// Baseline: with no profile bound, the mock tool is visible.
	_, baseSid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		ClientInfo:      protocol.Implementation{Name: "base", Version: "0"},
	}))
	baseList, _ := rpcPost(t, srv.URL, baseSid, newReq(2, protocol.MethodToolsList, struct{}{}))
	var base protocol.ListToolsResult
	_ = json.Unmarshal(baseList.Result, &base)
	if !toolPresent(base.Tools, "mock.echo") {
		t.Fatalf("baseline tools/list missing mock.echo (profile filtering can't be proven): %+v", base.Tools)
	}

	// Create a profile that allows a DIFFERENT server, then bind the dev principal.
	var created struct {
		ID string `json:"id"`
	}
	restJSON(t, srv, "POST", "/api/agent-profiles",
		`{"name":"restricted","allowed_mcp_servers":["other-server"],"allowed_tools":[],"allowed_skills":[],"allowed_model_aliases":[],"scopes":[],"enabled":true}`,
		http.StatusCreated, &created)
	if created.ID == "" {
		t.Fatal("create returned no profile id")
	}
	restJSON(t, srv, "PUT", "/api/agent-profiles/"+created.ID+"/bindings/dev", "", http.StatusNoContent, nil)

	// A fresh session now resolves the dev principal → the restricted profile.
	_, sid := rpcPost(t, srv.URL, "", newReq(1, protocol.MethodInitialize, protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		ClientInfo:      protocol.Implementation{Name: "plain", Version: "0"},
	}))
	listResp, _ := rpcPost(t, srv.URL, sid, newReq(2, protocol.MethodToolsList, struct{}{}))
	var list protocol.ListToolsResult
	_ = json.Unmarshal(listResp.Result, &list)
	if toolPresent(list.Tools, "mock.echo") {
		t.Fatalf("mock.echo not filtered out by the bound profile: %+v", list.Tools)
	}

	// tools/call on the out-of-surface tool is rejected with the typed violation.
	args, _ := json.Marshal(map[string]string{"message": "x"})
	callResp, _ := rpcPost(t, srv.URL, sid, newReq(3, protocol.MethodToolsCall, protocol.CallToolParams{
		Name: "mock.echo", Arguments: args,
	}))
	if callResp.Error == nil {
		t.Fatal("tools/call to an out-of-profile tool was not rejected")
	}
	if callResp.Error.Code != protocol.ErrAgentProfileViolation {
		t.Fatalf("want ErrAgentProfileViolation (%d), got %d: %s", protocol.ErrAgentProfileViolation, callResp.Error.Code, callResp.Error.Data)
	}
}

func toolPresent(tools []protocol.Tool, name string) bool {
	for _, tl := range tools {
		if tl.Name == name {
			return true
		}
	}
	return false
}
