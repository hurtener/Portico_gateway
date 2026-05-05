package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

func TestRequest_RoundTrip_NumericID(t *testing.T) {
	in := protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      json.RawMessage(`42`),
		Method:  protocol.MethodToolsList,
		Params:  json.RawMessage(`{}`),
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out protocol.Request
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if string(out.ID) != "42" {
		t.Errorf("ID = %s", out.ID)
	}
	if out.Method != protocol.MethodToolsList {
		t.Errorf("Method = %s", out.Method)
	}
}

func TestRequest_StringID(t *testing.T) {
	in := protocol.Request{JSONRPC: "2.0", ID: json.RawMessage(`"abc"`), Method: "ping"}
	body, _ := json.Marshal(in)
	var out protocol.Request
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if string(out.ID) != `"abc"` {
		t.Errorf("ID = %s", out.ID)
	}
}

func TestRequest_IsNotification(t *testing.T) {
	cases := []struct {
		name string
		r    protocol.Request
		want bool
	}{
		{"no id", protocol.Request{Method: "x"}, true},
		{"null id", protocol.Request{ID: json.RawMessage(`null`), Method: "x"}, true},
		{"numeric id", protocol.Request{ID: json.RawMessage(`1`), Method: "x"}, false},
		{"string id", protocol.Request{ID: json.RawMessage(`"k"`), Method: "x"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.r.IsNotification(); got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestError_RoundTrip(t *testing.T) {
	e := protocol.NewError(protocol.ErrUpstreamUnavailable, "downstream off", map[string]string{"server_id": "github"})
	body, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var out protocol.Error
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Code != protocol.ErrUpstreamUnavailable {
		t.Errorf("code = %d", out.Code)
	}
	if !contains(string(out.Data), "github") {
		t.Errorf("data missing server_id: %s", out.Data)
	}
}

func TestAggregateServerCaps(t *testing.T) {
	caps := []protocol.ServerCapabilities{
		{Tools: &protocol.ToolsCapability{ListChanged: true}},
		{Resources: &protocol.ResourcesCapability{ListChanged: true}},
		{Prompts: &protocol.PromptsCapability{}},
	}
	agg := protocol.AggregateServerCaps(caps)
	if agg.Tools == nil || !agg.Tools.ListChanged {
		t.Error("expected tools.listChanged=true")
	}
	if agg.Resources == nil || !agg.Resources.ListChanged {
		t.Error("expected resources.listChanged=true")
	}
	if agg.Prompts == nil {
		t.Error("expected prompts cap present")
	}
}

func TestRecordClientCaps(t *testing.T) {
	c := protocol.ClientCapabilities{
		Elicitation: &protocol.ElicitCapability{},
		Sampling:    &protocol.SamplingCapability{},
	}
	rec := protocol.RecordClientCaps(c)
	if !rec.HasElicitation || !rec.HasSampling {
		t.Errorf("rec = %+v", rec)
	}
	if rec.HasRoots {
		t.Error("HasRoots should be false")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
