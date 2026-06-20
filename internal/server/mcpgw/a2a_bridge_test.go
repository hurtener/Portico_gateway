package mcpgw

import (
	"context"
	"encoding/json"
	"testing"

	a2aproto "github.com/hurtener/Portico_gateway/internal/a2a/protocol"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/profiles"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

type fakeA2ABridge struct {
	lastTenant string
	lastPeer   string
	lastParams a2aproto.MessageSendParams
	result     json.RawMessage
	aerr       *a2aproto.Error
}

func (f *fakeA2ABridge) SendMessageByPeerName(_ context.Context, tenantID, peerName string, params a2aproto.MessageSendParams) (json.RawMessage, *a2aproto.Error) {
	f.lastTenant, f.lastPeer, f.lastParams = tenantID, peerName, params
	return f.result, f.aerr
}

func bridgedProfile() *profiles.Profile {
	return &profiles.Profile{
		TenantID:        "t1",
		ID:              "ap_1",
		AllowedA2APeers: []string{"research-agent"},
		MCPToA2ABridges: []ifaces.MCPToA2ABridge{
			{MCPTool: "github.code-review.run", A2APeer: "research-agent", A2ATask: "code-review"},
		},
	}
}

func TestBridge_NoBridgeConfigured(t *testing.T) {
	d := &Dispatcher{} // a2aBridge nil
	ctx := profiles.WithProfile(context.Background(), bridgedProfile())
	_, _, bridged := d.tryMCPToA2ABridge(ctx, &Session{ID: "s1", TenantID: "t1"}, protocol.CallToolParams{Name: "github.code-review.run"}, "r1")
	if bridged {
		t.Fatal("no a2aBridge wired → must not bridge")
	}
}

func TestBridge_ToolNotBridged(t *testing.T) {
	d := &Dispatcher{a2aBridge: &fakeA2ABridge{}}
	ctx := profiles.WithProfile(context.Background(), bridgedProfile())
	_, _, bridged := d.tryMCPToA2ABridge(ctx, &Session{ID: "s1", TenantID: "t1"}, protocol.CallToolParams{Name: "other.tool"}, "r1")
	if bridged {
		t.Fatal("unbridged tool must route over MCP")
	}
}

func TestBridge_DispatchesAndTranslates(t *testing.T) {
	taskJSON := json.RawMessage(`{"id":"task-1","kind":"task","status":{"state":"completed"}}`)
	fake := &fakeA2ABridge{result: taskJSON}
	d := &Dispatcher{a2aBridge: fake}
	ctx := profiles.WithProfile(context.Background(), bridgedProfile())

	body, perr, bridged := d.tryMCPToA2ABridge(ctx, &Session{ID: "s1", TenantID: "t1"},
		protocol.CallToolParams{Name: "github.code-review.run", Arguments: json.RawMessage(`{"diff":"x"}`)}, "r1")
	if !bridged || perr != nil {
		t.Fatalf("expected bridged success, got bridged=%v perr=%+v", bridged, perr)
	}
	if fake.lastPeer != "research-agent" || fake.lastTenant != "t1" {
		t.Errorf("dispatched to tenant=%q peer=%q", fake.lastTenant, fake.lastPeer)
	}
	// The MCP args became an A2A data part.
	if len(fake.lastParams.Message.Parts) != 1 || fake.lastParams.Message.Parts[0].Kind != a2aproto.PartKindData {
		t.Errorf("args not translated to a data part: %+v", fake.lastParams.Message.Parts)
	}
	if fake.lastParams.Message.Parts[0].Data["diff"] != "x" {
		t.Errorf("arg value lost: %+v", fake.lastParams.Message.Parts[0].Data)
	}
	// The A2A result became an MCP CallToolResult carrying the JSON.
	var res protocol.CallToolResult
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decode CallToolResult: %v", err)
	}
	if string(res.StructuredContent) != string(taskJSON) {
		t.Errorf("structuredContent = %s, want the A2A task JSON", res.StructuredContent)
	}
	if len(res.Content) != 1 || res.Content[0].Type != "text" {
		t.Errorf("content = %+v", res.Content)
	}
}

func TestBridge_TaskDeniedByProfile(t *testing.T) {
	prof := bridgedProfile()
	prof.AllowedA2APeers = []string{"other-agent"} // research-agent (the bridge target) not allowed
	d := &Dispatcher{a2aBridge: &fakeA2ABridge{result: json.RawMessage(`{}`)}}
	ctx := profiles.WithProfile(context.Background(), prof)
	_, perr, bridged := d.tryMCPToA2ABridge(ctx, &Session{ID: "s1", TenantID: "t1"},
		protocol.CallToolParams{Name: "github.code-review.run"}, "r1")
	if !bridged {
		t.Fatal("a denied bridge still terminates the call (bridged=true)")
	}
	if perr == nil || perr.Code != protocol.ErrAgentProfileViolation {
		t.Fatalf("want ErrAgentProfileViolation, got %+v", perr)
	}
}

func TestBridge_DispatchErrorMapsToUpstream(t *testing.T) {
	fake := &fakeA2ABridge{aerr: a2aproto.NewError(a2aproto.ErrTaskNotFound, "no such task")}
	d := &Dispatcher{a2aBridge: fake}
	ctx := profiles.WithProfile(context.Background(), bridgedProfile())
	_, perr, bridged := d.tryMCPToA2ABridge(ctx, &Session{ID: "s1", TenantID: "t1"},
		protocol.CallToolParams{Name: "github.code-review.run"}, "r1")
	if !bridged || perr == nil || perr.Code != protocol.ErrUpstreamUnavailable {
		t.Fatalf("want ErrUpstreamUnavailable, got bridged=%v perr=%+v", bridged, perr)
	}
}
