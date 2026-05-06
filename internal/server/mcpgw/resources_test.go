package mcpgw

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/apps"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/mcp/southbound"
	southboundmgr "github.com/hurtener/Portico_gateway/internal/mcp/southbound/manager"
	"github.com/hurtener/Portico_gateway/internal/registry"
	"github.com/hurtener/Portico_gateway/internal/storage/ifaces"
)

// fakeFleet is the minimum clientFleet implementation needed by the
// resource + prompt aggregators. clients keyed by serverID; missing
// servers are simulated by omitting them.
type fakeFleet struct {
	servers map[string]*registry.Snapshot
	clients map[string]*fakeSouthClient
}

func (f *fakeFleet) Servers(_ context.Context, _ string) ([]*registry.Snapshot, error) {
	out := make([]*registry.Snapshot, 0, len(f.servers))
	for _, s := range f.servers {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeFleet) Acquire(_ context.Context, req southboundmgr.AcquireRequest) (southbound.Client, error) {
	c, ok := f.clients[req.ServerID]
	if !ok {
		return nil, errors.New("fake: unknown server " + req.ServerID)
	}
	return c, nil
}

// fakeSouthClient implements southbound.Client with canned responses.
type fakeSouthClient struct {
	resources []protocol.Resource
	prompts   []protocol.Prompt
	templates []protocol.ResourceTemplate
	read      func(uri string) (*protocol.ReadResourceResult, error)
	getPrompt func(name string, args map[string]string) (*protocol.GetPromptResult, error)

	listResourcesErr error
	listPromptsErr   error

	notifs chan protocol.Notification
}

func (c *fakeSouthClient) Start(_ context.Context) error { return nil }
func (c *fakeSouthClient) Initialized() bool             { return true }
func (c *fakeSouthClient) Capabilities() protocol.ServerCapabilities {
	return protocol.ServerCapabilities{}
}
func (c *fakeSouthClient) ServerInfo() protocol.Implementation {
	return protocol.Implementation{Name: "fake"}
}
func (c *fakeSouthClient) Ping(_ context.Context) error { return nil }
func (c *fakeSouthClient) ListTools(_ context.Context) ([]protocol.Tool, error) {
	return nil, nil
}
func (c *fakeSouthClient) CallTool(_ context.Context, _ string, _ json.RawMessage, _ json.RawMessage, _ southbound.ProgressCallback) (*protocol.CallToolResult, error) {
	return nil, nil
}
func (c *fakeSouthClient) ListResources(_ context.Context, _ string) ([]protocol.Resource, string, error) {
	return c.resources, "", c.listResourcesErr
}
func (c *fakeSouthClient) ListResourceTemplates(_ context.Context, _ string) ([]protocol.ResourceTemplate, string, error) {
	return c.templates, "", nil
}
func (c *fakeSouthClient) ReadResource(_ context.Context, uri string) (*protocol.ReadResourceResult, error) {
	if c.read != nil {
		return c.read(uri)
	}
	return &protocol.ReadResourceResult{}, nil
}
func (c *fakeSouthClient) SubscribeResource(_ context.Context, _ string) error   { return nil }
func (c *fakeSouthClient) UnsubscribeResource(_ context.Context, _ string) error { return nil }
func (c *fakeSouthClient) ListPrompts(_ context.Context, _ string) ([]protocol.Prompt, string, error) {
	return c.prompts, "", c.listPromptsErr
}
func (c *fakeSouthClient) GetPrompt(_ context.Context, name string, args map[string]string) (*protocol.GetPromptResult, error) {
	if c.getPrompt != nil {
		return c.getPrompt(name, args)
	}
	return &protocol.GetPromptResult{}, nil
}
func (c *fakeSouthClient) Notifications() <-chan protocol.Notification {
	if c.notifs == nil {
		c.notifs = make(chan protocol.Notification)
	}
	return c.notifs
}
func (c *fakeSouthClient) Close(_ context.Context) error { return nil }

// snapshotFor crafts a minimal registry snapshot for a server id.
func snapshotFor(id string) *registry.Snapshot {
	return &registry.Snapshot{
		Spec: registry.ServerSpec{ID: id, Transport: "stdio", RuntimeMode: registry.ModeSharedGlobal},
		Record: ifaces.ServerRecord{
			ID:       id,
			TenantID: "acme",
			Enabled:  true,
		},
	}
}

func newTestSession() *Session {
	return &Session{ID: "s1", TenantID: "acme"}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ----- ListAll -------------------------------------------------------------

func TestResources_ListAll_TwoServers_Aggregated(t *testing.T) {
	fleet := &fakeFleet{
		servers: map[string]*registry.Snapshot{
			"github":   snapshotFor("github"),
			"postgres": snapshotFor("postgres"),
		},
		clients: map[string]*fakeSouthClient{
			"github": {resources: []protocol.Resource{
				{URI: "file:///README.md", Name: "readme"},
				{URI: "ui://review-panel.html", MimeType: "text/html"},
			}},
			"postgres": {resources: []protocol.Resource{
				{URI: "https://docs.postgresql.org/manual"},
			}},
		},
	}
	appsReg := apps.New(apps.CSPConfig{})
	agg := NewResourceAggregator(fleet, appsReg, ResourceLimits{}, discardLogger())
	res, err := agg.ListAll(context.Background(), newTestSession(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Resources) != 3 {
		t.Fatalf("aggregated resources = %d; want 3", len(res.Resources))
	}
	// URIs should be namespaced (sorted).
	wantPrefixes := []string{"mcp+server://", "ui://github/"}
	for _, r := range res.Resources {
		matched := false
		for _, p := range wantPrefixes {
			if strings.HasPrefix(r.URI, p) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("resource not namespaced: %q", r.URI)
		}
	}
	// Apps registry should hold the ui:// entry.
	if app, ok := appsReg.Lookup("ui://github/review-panel.html"); !ok || app.ServerID != "github" {
		t.Errorf("apps registry didn't capture ui:// resource")
	}
}

func TestResources_ListAll_ToleratesPartialFailure(t *testing.T) {
	fleet := &fakeFleet{
		servers: map[string]*registry.Snapshot{
			"a": snapshotFor("a"),
			"b": snapshotFor("b"),
		},
		clients: map[string]*fakeSouthClient{
			"a": {listResourcesErr: errors.New("upstream timeout")},
			"b": {resources: []protocol.Resource{{URI: "file:///x.md"}}},
		},
	}
	agg := NewResourceAggregator(fleet, nil, ResourceLimits{}, discardLogger())
	res, err := agg.ListAll(context.Background(), newTestSession(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Resources) != 1 {
		t.Errorf("partial failure should still return b's resource; got %d", len(res.Resources))
	}
}

// ----- Read ---------------------------------------------------------------

func TestResources_Read_RoutesByPrefix(t *testing.T) {
	fleet := &fakeFleet{
		servers: map[string]*registry.Snapshot{"github": snapshotFor("github")},
		clients: map[string]*fakeSouthClient{
			"github": {
				read: func(uri string) (*protocol.ReadResourceResult, error) {
					if uri != "file:///README.md" {
						return nil, errors.New("wrong restored uri: " + uri)
					}
					return &protocol.ReadResourceResult{Contents: []protocol.ResourceContent{
						{URI: uri, MimeType: "text/markdown", Text: "hi"},
					}}, nil
				},
			},
		},
	}
	agg := NewResourceAggregator(fleet, nil, ResourceLimits{}, discardLogger())
	res, err := agg.Read(context.Background(), newTestSession(), "mcp+server://github/file/README.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Contents) != 1 || res.Contents[0].Text != "hi" {
		t.Errorf("read result = %+v", res.Contents)
	}
	// Ensure the URI was re-namespaced on the way out.
	if !strings.HasPrefix(res.Contents[0].URI, "mcp+server://github/") {
		t.Errorf("output URI not re-namespaced: %q", res.Contents[0].URI)
	}
}

func TestResources_Read_RejectsBareURI(t *testing.T) {
	fleet := &fakeFleet{
		servers: map[string]*registry.Snapshot{"github": snapshotFor("github")},
		clients: map[string]*fakeSouthClient{"github": {}},
	}
	agg := NewResourceAggregator(fleet, nil, ResourceLimits{}, discardLogger())
	_, err := agg.Read(context.Background(), newTestSession(), "https://raw.example.com/x")
	if err == nil {
		t.Errorf("expected rejection for non-namespaced URI")
	}
}

func TestResources_Read_TruncatesOversized(t *testing.T) {
	bigText := strings.Repeat("x", 1024*1024+1) // 1 MB + 1 byte
	fleet := &fakeFleet{
		servers: map[string]*registry.Snapshot{"docs": snapshotFor("docs")},
		clients: map[string]*fakeSouthClient{
			"docs": {
				read: func(uri string) (*protocol.ReadResourceResult, error) {
					return &protocol.ReadResourceResult{Contents: []protocol.ResourceContent{
						{URI: uri, MimeType: "text/plain", Text: bigText},
					}}, nil
				},
			},
		},
	}
	agg := NewResourceAggregator(fleet, nil, ResourceLimits{MaxBytesPerRead: 1024 * 1024}, discardLogger())
	res, err := agg.Read(context.Background(), newTestSession(), "mcp+server://docs/file/big.txt")
	if err != nil {
		t.Fatal(err)
	}
	c := res.Contents[0]
	if int64(len(c.Text)) != 1024*1024 {
		t.Errorf("truncation: text len = %d", len(c.Text))
	}
	var meta map[string]any
	if err := json.Unmarshal(c.Meta, &meta); err != nil {
		t.Fatalf("bad meta json: %v", err)
	}
	portico, _ := meta["portico"].(map[string]any)
	if portico == nil || portico["truncated"] != "true" {
		t.Errorf("truncation flag missing: %+v", meta)
	}
	if !strings.HasPrefix(portico["artifact_uri"].(string), "artifact://") {
		t.Errorf("artifact_uri missing: %+v", portico)
	}
}

// ----- CSP wrapping on ui:// + text/html ----------------------------------

func TestResources_Read_WrapsHTMLForUIScheme(t *testing.T) {
	html := `<!doctype html><html><head><title>panel</title></head><body>hi</body></html>`
	fleet := &fakeFleet{
		servers: map[string]*registry.Snapshot{"github": snapshotFor("github")},
		clients: map[string]*fakeSouthClient{
			"github": {
				read: func(uri string) (*protocol.ReadResourceResult, error) {
					return &protocol.ReadResourceResult{Contents: []protocol.ResourceContent{
						{URI: uri, MimeType: "text/html", Text: html},
					}}, nil
				},
			},
		},
	}
	agg := NewResourceAggregator(fleet, apps.New(apps.CSPConfig{}), ResourceLimits{}, discardLogger())
	res, err := agg.Read(context.Background(), newTestSession(), "ui://github/panel.html")
	if err != nil {
		t.Fatal(err)
	}
	body := res.Contents[0].Text
	if !strings.Contains(body, "Content-Security-Policy") {
		t.Errorf("CSP meta not injected: %s", body)
	}
}

// ----- Templates ----------------------------------------------------------

func TestResources_ListTemplates_Aggregates(t *testing.T) {
	fleet := &fakeFleet{
		servers: map[string]*registry.Snapshot{"a": snapshotFor("a")},
		clients: map[string]*fakeSouthClient{
			"a": {templates: []protocol.ResourceTemplate{
				{URITemplate: "file:///{path}"},
			}},
		},
	}
	agg := NewResourceAggregator(fleet, nil, ResourceLimits{}, discardLogger())
	res, err := agg.ListTemplates(context.Background(), newTestSession(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ResourceTemplates) != 1 {
		t.Fatalf("templates = %d", len(res.ResourceTemplates))
	}
	if !strings.HasPrefix(res.ResourceTemplates[0].URITemplate, "mcp+server://a/") {
		t.Errorf("template not namespaced: %q", res.ResourceTemplates[0].URITemplate)
	}
}

// ----- Cursor -------------------------------------------------------------

func TestResources_AggregatorCursor_Roundtrip(t *testing.T) {
	encoded := encodeAggregatorCursor(map[string]string{"a": "x", "b": ""})
	decoded, err := decodeAggregatorCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded["a"] != "x" {
		t.Errorf("cursor lost data: %+v", decoded)
	}
	// b had empty cursor — encodeAggregatorCursor should still preserve it
	// since the input map said so explicitly. (List code never inserts
	// empty cursors so this is mostly defensive.)
	_ = base64.RawURLEncoding // keep import live if changes drop usage above
}

func TestResources_AggregatorCursor_RejectsGarbage(t *testing.T) {
	if _, err := decodeAggregatorCursor("not-base64$"); err == nil {
		t.Errorf("expected decode error")
	}
}
