package mcpgw

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/registry"
)

func TestPrompts_ListAll_PrefixesNames(t *testing.T) {
	fleet := &fakeFleet{
		servers: map[string]*registry.Snapshot{
			"a": snapshotFor("a"),
			"b": snapshotFor("b"),
		},
		clients: map[string]*fakeSouthClient{
			"a": {prompts: []protocol.Prompt{{Name: "summarize"}}},
			"b": {prompts: []protocol.Prompt{{Name: "review"}}},
		},
	}
	agg := NewPromptAggregator(fleet, nil, discardLogger())
	res, err := agg.ListAll(context.Background(), newTestSession(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Prompts) != 2 {
		t.Fatalf("prompts = %d", len(res.Prompts))
	}
	want := map[string]bool{"a.summarize": false, "b.review": false}
	for _, p := range res.Prompts {
		if _, ok := want[p.Name]; ok {
			want[p.Name] = true
		} else {
			t.Errorf("unexpected name %q", p.Name)
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("missing %q", k)
		}
	}
}

func TestPrompts_Get_StripsPrefixAndRoutes(t *testing.T) {
	fleet := &fakeFleet{
		servers: map[string]*registry.Snapshot{"github": snapshotFor("github")},
		clients: map[string]*fakeSouthClient{
			"github": {
				getPrompt: func(name string, _ map[string]string) (*protocol.GetPromptResult, error) {
					if name != "review" {
						return nil, errors.New("expected stripped name 'review', got " + name)
					}
					return &protocol.GetPromptResult{Description: "ok"}, nil
				},
			},
		},
	}
	agg := NewPromptAggregator(fleet, nil, discardLogger())
	res, err := agg.Get(context.Background(), newTestSession(), "github.review", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Description != "ok" {
		t.Errorf("descr = %q", res.Description)
	}
}

func TestPrompts_Get_RejectsBareName(t *testing.T) {
	fleet := &fakeFleet{
		servers: map[string]*registry.Snapshot{"github": snapshotFor("github")},
		clients: map[string]*fakeSouthClient{"github": {}},
	}
	agg := NewPromptAggregator(fleet, nil, discardLogger())
	_, err := agg.Get(context.Background(), newTestSession(), "bareprompt", nil)
	if err == nil {
		t.Errorf("expected rejection for unqualified name")
	} else if !strings.Contains(err.Error(), "<server>.<name>") {
		t.Errorf("unexpected error: %v", err)
	}
}
