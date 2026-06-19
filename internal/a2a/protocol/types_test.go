package protocol_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/a2a/protocol"
)

func TestA2AProtocol_AgentCard_RoundTrip(t *testing.T) {
	in := protocol.AgentCard{
		Name:               "research-agent",
		Description:        "Conducts research and writes reports",
		URL:                "https://research-agent.example.com/a2a",
		Version:            "1.4.2",
		ProtocolVersion:    protocol.SpecVersion,
		Provider:           &protocol.AgentProvider{Organization: "Acme Labs", URL: "https://acme.example.com"},
		Capabilities:       protocol.AgentCapabilities{Streaming: true, PushNotifications: true},
		DefaultInputModes:  []string{"text/plain", "application/json"},
		DefaultOutputModes: []string{"application/json"},
		Skills: []protocol.AgentSkill{
			{
				ID:          "code-review",
				Name:        "Code review",
				Description: "Reviews a code diff and returns a structured critique",
				Tags:        []string{"code", "review"},
				Examples:    []string{"Review this diff: ..."},
				InputModes:  []string{"text/plain"},
				OutputModes: []string{"application/json"},
			},
		},
		DocumentationURL:                  "https://research-agent.example.com/docs",
		SupportsAuthenticatedExtendedCard: true,
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out protocol.AgentCard
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Name != in.Name || out.Description != in.Description || out.URL != in.URL {
		t.Errorf("top-level fields: got %+v", out)
	}
	if out.Version != in.Version || out.ProtocolVersion != in.ProtocolVersion {
		t.Errorf("version fields: got %+v", out)
	}
	if out.Provider == nil || out.Provider.Organization != in.Provider.Organization || out.Provider.URL != in.Provider.URL {
		t.Errorf("provider: got %+v", out.Provider)
	}
	if !out.Capabilities.Streaming || !out.Capabilities.PushNotifications {
		t.Errorf("capabilities: got %+v", out.Capabilities)
	}
	if !out.SupportsAuthenticatedExtendedCard {
		t.Errorf("SupportsAuthenticatedExtendedCard lost in round-trip")
	}
	if len(out.Skills) != 1 || out.Skills[0].ID != "code-review" {
		t.Fatalf("skills: got %+v", out.Skills)
	}
	if out.Skills[0].Tags[0] != "code" || out.Skills[0].Examples[0] != "Review this diff: ..." {
		t.Errorf("nested skill fields: %+v", out.Skills[0])
	}
	if len(out.DefaultInputModes) != 2 || out.DefaultInputModes[1] != "application/json" {
		t.Errorf("default input modes: %+v", out.DefaultInputModes)
	}
}

func TestA2AProtocol_Envelope_RoundTrip(t *testing.T) {
	// Request with raw params.
	req := protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`7`),
		Method:  protocol.MethodMessageSend,
		Params:  json.RawMessage(`{"message":{"parts":[{"type":"text","text":"hello"}]}}`),
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var reqOut protocol.Request
	if err := json.Unmarshal(body, &reqOut); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if string(reqOut.ID) != "7" {
		t.Errorf("id = %s", reqOut.ID)
	}
	if reqOut.Method != protocol.MethodMessageSend {
		t.Errorf("method = %s", reqOut.Method)
	}
	if reqOut.IsNotification() {
		t.Errorf("Request with id 7 reported as notification")
	}
	if !strings.Contains(string(reqOut.Params), `"hello"`) {
		t.Errorf("params lost text content: %s", reqOut.Params)
	}

	// Response carrying an error.
	resp := protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`"req-abc"`),
		Error:   protocol.NewError(protocol.ErrTaskNotFound, "no such task: 42"),
	}
	body, err = json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var respOut protocol.Response
	if err := json.Unmarshal(body, &respOut); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if respOut.Error == nil {
		t.Fatal("expected error on response")
	}
	if respOut.Error.Code != protocol.ErrTaskNotFound {
		t.Errorf("error.code = %d", respOut.Error.Code)
	}
	if respOut.Error.Error() != "no such task: 42" {
		t.Errorf("Error() = %s", respOut.Error.Error())
	}
}

func TestA2AProtocol_Notification_IsNotification(t *testing.T) {
	cases := []struct {
		name string
		r    protocol.Request
		want bool
	}{
		{"no id", protocol.Request{Method: protocol.MethodMessageSend}, true},
		{"null id", protocol.Request{ID: json.RawMessage(`null`), Method: protocol.MethodMessageSend}, true},
		{"numeric id", protocol.Request{ID: json.RawMessage(`1`), Method: protocol.MethodMessageSend}, false},
		{"string id", protocol.Request{ID: json.RawMessage(`"k"`), Method: protocol.MethodMessageSend}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.r.IsNotification(); got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestA2AProtocol_Error_NilSafeError(t *testing.T) {
	var nilErr *protocol.Error
	if got := nilErr.Error(); got == "" {
		t.Errorf("nil error should render non-empty: %q", got)
	}
}
