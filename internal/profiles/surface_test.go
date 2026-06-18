package profiles

import (
	"reflect"
	"testing"
)

func TestMaterialize_LiveIntersection(t *testing.T) {
	p := &Profile{
		ID:                  "ap_1",
		AllowedMCPServers:   []string{"github", "jira"},
		AllowedTools:        []string{"github.list_issues"},
		AllowedSkills:       []string{"code-review"},
		AllowedModelAliases: []string{"gpt-4o"},
	}
	// Live catalog: jira deregistered (absent), an extra server present, an
	// extra skill + alias the profile doesn't allow.
	live := LiveCatalog{
		Servers: []string{"github", "slack"},
		Skills:  []string{"code-review", "triage"},
		Aliases: []string{"gpt-4o", "claude-3-5-sonnet"},
	}
	got := p.Materialize(live)

	if !reflect.DeepEqual(got.Servers, []string{"github"}) {
		t.Errorf("servers: got %v, want [github] (jira deregistered, slack not allowed)", got.Servers)
	}
	if !reflect.DeepEqual(got.Tools, []string{"github.list_issues"}) {
		t.Errorf("tools: got %v", got.Tools)
	}
	if !reflect.DeepEqual(got.Skills, []string{"code-review"}) {
		t.Errorf("skills: got %v", got.Skills)
	}
	if !reflect.DeepEqual(got.Models, []string{"gpt-4o"}) {
		t.Errorf("models: got %v", got.Models)
	}
	if got.ProfileID != "ap_1" || got.IsDefault {
		t.Errorf("meta wrong: %+v", got)
	}
}

func TestMaterialize_DefaultProfile_FullCatalog(t *testing.T) {
	def := DefaultProfile("t1")
	live := LiveCatalog{
		Servers: []string{"github", "jira"},
		Skills:  []string{"code-review"},
		Aliases: []string{"gpt-4o"},
	}
	got := def.Materialize(live)
	if !reflect.DeepEqual(got.Servers, []string{"github", "jira"}) {
		t.Errorf("default must see all live servers: %v", got.Servers)
	}
	if !reflect.DeepEqual(got.Skills, []string{"code-review"}) {
		t.Errorf("default must see all live skills: %v", got.Skills)
	}
	if !reflect.DeepEqual(got.Models, []string{"gpt-4o"}) {
		t.Errorf("default must see all live aliases: %v", got.Models)
	}
	if len(got.Tools) != 0 {
		t.Errorf("default declares no tool allowlist: %v", got.Tools)
	}
	if !got.IsDefault {
		t.Error("default surface must flag IsDefault")
	}
}

func TestMaterialize_EmptyToolAllowlist(t *testing.T) {
	// Empty AllowedTools means "all tools in allowed servers" — not enumerable
	// here, so the materialised tool list is empty.
	p := &Profile{ID: "ap_2", AllowedMCPServers: []string{"github"}}
	got := p.Materialize(LiveCatalog{Servers: []string{"github"}})
	if len(got.Tools) != 0 {
		t.Errorf("empty declared allowlist must materialise to empty tools: %v", got.Tools)
	}
	if !reflect.DeepEqual(got.Servers, []string{"github"}) {
		t.Errorf("servers: %v", got.Servers)
	}
}

// TestIntersection_MostRestrictiveWins documents the canonical V2 rule: when
// the profile's allowlist meets a (narrower OR wider) live catalog, the
// effective surface is the intersection — the most-restrictive layer always
// wins, the profile never broadens what the live catalog offers and the live
// catalog never broadens what the profile allows.
func TestIntersection_MostRestrictiveWins(t *testing.T) {
	// Profile allows a WIDE set; the live catalog is NARROWER → live wins.
	wide := &Profile{ID: "ap_1", AllowedMCPServers: []string{"github", "jira", "slack"}}
	got := wide.Materialize(LiveCatalog{Servers: []string{"github"}})
	if !reflect.DeepEqual(got.Servers, []string{"github"}) {
		t.Fatalf("a narrower live catalog must win: %v", got.Servers)
	}

	// Profile allows a NARROW set; the live catalog is WIDER → profile wins.
	narrow := &Profile{ID: "ap_2", AllowedMCPServers: []string{"github"}}
	got = narrow.Materialize(LiveCatalog{Servers: []string{"github", "jira", "slack"}})
	if !reflect.DeepEqual(got.Servers, []string{"github"}) {
		t.Fatalf("a narrower profile must win: %v", got.Servers)
	}

	// AllowsServer/AllowsTool are the gate every layer routes through, so a
	// restrictive profile can only ever subtract.
	if narrow.AllowsServer("jira") {
		t.Fatal("profile must not allow a server outside its allowlist (no broadening)")
	}
}

func TestMaterialize_DropsToolWhoseServerNotAllowed(t *testing.T) {
	// A declared tool whose server is not in the allowlist is dropped.
	p := &Profile{ID: "ap_3", AllowedMCPServers: []string{"github"}, AllowedTools: []string{"github.x", "jira.y"}}
	got := p.Materialize(LiveCatalog{Servers: []string{"github", "jira"}})
	if !reflect.DeepEqual(got.Tools, []string{"github.x"}) {
		t.Errorf("tools must drop jira.y (server not allowed): %v", got.Tools)
	}
}
