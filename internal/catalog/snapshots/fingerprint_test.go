package snapshots_test

import (
	"encoding/json"
	"math/rand"
	"testing"
	"time"

	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

func sampleTools() []protocol.Tool {
	return []protocol.Tool{
		{
			Name:        "alpha",
			Description: "first tool",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`),
		},
		{
			Name:        "bravo",
			Description: "second tool",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		},
		{
			Name:        "charlie",
			Description: "third tool",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"}}}`),
		},
	}
}

func cloneTools(in []protocol.Tool) []protocol.Tool {
	out := make([]protocol.Tool, len(in))
	copy(out, in)
	return out
}

func shuffle[T any](r *rand.Rand, in []T) []T {
	out := make([]T, len(in))
	copy(out, in)
	r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

func TestServerToolsFingerprint_Stable(t *testing.T) {
	t.Parallel()
	a := snapshots.ServerToolsFingerprint(sampleTools())
	b := snapshots.ServerToolsFingerprint(sampleTools())
	if a != b {
		t.Errorf("ServerToolsFingerprint not stable across calls: %s vs %s", a, b)
	}
	if a == "" {
		t.Error("ServerToolsFingerprint returned empty hash")
	}
}

func TestServerToolsFingerprint_OrderInvariant(t *testing.T) {
	t.Parallel()
	base := sampleTools()
	want := snapshots.ServerToolsFingerprint(base)
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 20; i++ {
		shuffled := shuffle(r, base)
		got := snapshots.ServerToolsFingerprint(shuffled)
		if got != want {
			t.Errorf("ServerToolsFingerprint not order invariant on iteration %d: %s vs %s", i, got, want)
		}
	}
}

func TestServerToolsFingerprint_OneToolChange_DifferentHash(t *testing.T) {
	t.Parallel()
	base := sampleTools()
	mod := cloneTools(base)
	mod[0].Description = "first tool MUTATED"

	if snapshots.ServerToolsFingerprint(base) == snapshots.ServerToolsFingerprint(mod) {
		t.Error("changing description must change the per-server fingerprint")
	}
}

func TestServerToolsFingerprint_NewToolAdded_DifferentHash(t *testing.T) {
	t.Parallel()
	base := sampleTools()
	added := append(cloneTools(base), protocol.Tool{Name: "delta", Description: "new"})
	if snapshots.ServerToolsFingerprint(base) == snapshots.ServerToolsFingerprint(added) {
		t.Error("adding a tool must change the per-server fingerprint")
	}
}

func TestToolFingerprint_AllFieldsContribute(t *testing.T) {
	t.Parallel()

	readOnly := true
	otherReadOnly := false
	base := snapshots.ToolInfo{
		NamespacedName:   "github.create_issue",
		ServerID:         "github",
		Description:      "Create an issue",
		InputSchema:      json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`),
		Annotations:      &protocol.ToolAnnotations{Title: "Create issue", ReadOnlyHint: &readOnly},
		RiskClass:        "medium",
		RequiresApproval: false,
		SkillID:          "skill_a",
	}

	cases := []struct {
		name   string
		mutate func(*snapshots.ToolInfo)
	}{
		{
			name:   "description",
			mutate: func(t *snapshots.ToolInfo) { t.Description = "different" },
		},
		{
			name: "input_schema",
			mutate: func(t *snapshots.ToolInfo) {
				t.InputSchema = json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string"}}}`)
			},
		},
		{
			name: "annotations",
			mutate: func(t *snapshots.ToolInfo) {
				t.Annotations = &protocol.ToolAnnotations{Title: "Create issue", ReadOnlyHint: &otherReadOnly}
			},
		},
		{
			name:   "risk_class",
			mutate: func(t *snapshots.ToolInfo) { t.RiskClass = "high" },
		},
		{
			name:   "requires_approval",
			mutate: func(t *snapshots.ToolInfo) { t.RequiresApproval = true },
		},
		{
			name:   "skill_id",
			mutate: func(t *snapshots.ToolInfo) { t.SkillID = "skill_b" },
		},
	}

	baseHash := snapshots.ToolFingerprint(base)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mod := base
			tc.mutate(&mod)
			got := snapshots.ToolFingerprint(mod)
			if got == baseHash {
				t.Errorf("changing %s did not change the tool fingerprint", tc.name)
			}
		})
	}
}

func sampleSnapshot() *snapshots.Snapshot {
	tools := []snapshots.ToolInfo{
		{
			NamespacedName:   "github.create_issue",
			ServerID:         "github",
			Description:      "Create an issue",
			InputSchema:      json.RawMessage(`{"type":"object"}`),
			RiskClass:        "low",
			RequiresApproval: false,
			SkillID:          "",
		},
		{
			NamespacedName:   "github.delete_repo",
			ServerID:         "github",
			Description:      "Delete a repository",
			InputSchema:      json.RawMessage(`{"type":"object"}`),
			RiskClass:        "high",
			RequiresApproval: true,
			SkillID:          "",
		},
		{
			NamespacedName:   "linear.list_issues",
			ServerID:         "linear",
			Description:      "List Linear issues",
			InputSchema:      json.RawMessage(`{"type":"object"}`),
			RiskClass:        "low",
			RequiresApproval: false,
			SkillID:          "",
		},
	}
	for i := range tools {
		tools[i].Hash = snapshots.ToolFingerprint(tools[i])
	}

	return &snapshots.Snapshot{
		ID:        "snap_initial",
		TenantID:  "acme",
		SessionID: "sess_1",
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Servers: []snapshots.ServerInfo{
			{ID: "github", Transport: "http", SchemaHash: "h-github", Health: "ok"},
			{ID: "linear", Transport: "http", SchemaHash: "h-linear", Health: "ok"},
		},
		Tools: tools,
		Resources: []snapshots.ResourceInfo{
			{URI: "github://repo/x", UpstreamURI: "x", ServerID: "github"},
			{URI: "linear://issue/1", UpstreamURI: "1", ServerID: "linear"},
		},
		Prompts: []snapshots.PromptInfo{
			{NamespacedName: "github.summarise", ServerID: "github"},
			{NamespacedName: "linear.weekly", ServerID: "linear"},
		},
		Skills: []snapshots.SkillInfo{
			{ID: "skill_a", Version: "1.0.0", EnabledForSession: true},
			{ID: "skill_b", Version: "0.5.0", EnabledForSession: false},
		},
		Credentials: []snapshots.CredentialInfo{
			{ServerID: "github", Strategy: "oauth", SecretRefs: []string{"github-token"}},
			{ServerID: "linear", Strategy: "header", SecretRefs: []string{"linear-key"}},
		},
		Policies: snapshots.PoliciesInfo{DefaultRiskClass: "medium"},
	}
}

func TestOverallFingerprint_DeterministicAcrossPermutations(t *testing.T) {
	t.Parallel()
	base := sampleSnapshot()
	want := snapshots.OverallFingerprint(base)

	r := rand.New(rand.NewSource(42))
	for i := 0; i < 50; i++ {
		permuted := *base
		permuted.Servers = shuffle(r, base.Servers)
		permuted.Tools = shuffle(r, base.Tools)
		permuted.Resources = shuffle(r, base.Resources)
		permuted.Prompts = shuffle(r, base.Prompts)
		permuted.Skills = shuffle(r, base.Skills)
		permuted.Credentials = shuffle(r, base.Credentials)
		got := snapshots.OverallFingerprint(&permuted)
		if got != want {
			t.Fatalf("permutation %d: hash diverged: got %s, want %s", i, got, want)
		}
	}
}

func TestOverallFingerprint_ExcludesIDAndCreatedAt(t *testing.T) {
	t.Parallel()
	a := sampleSnapshot()
	b := sampleSnapshot()
	b.ID = "snap_other"
	b.CreatedAt = a.CreatedAt.Add(72 * time.Hour)

	if snapshots.OverallFingerprint(a) != snapshots.OverallFingerprint(b) {
		t.Error("OverallFingerprint must ignore ID and CreatedAt")
	}
}

func TestDiffSnapshots_AddedRemovedModified(t *testing.T) {
	t.Parallel()
	a := sampleSnapshot()
	b := sampleSnapshot()

	// Remove "github.delete_repo".
	filtered := make([]snapshots.ToolInfo, 0, len(b.Tools))
	for _, ti := range b.Tools {
		if ti.NamespacedName == "github.delete_repo" {
			continue
		}
		filtered = append(filtered, ti)
	}
	b.Tools = filtered

	// Modify "github.create_issue": change description (and re-hash).
	for i := range b.Tools {
		if b.Tools[i].NamespacedName == "github.create_issue" {
			b.Tools[i].Description = "Create a GH issue (renamed)"
			b.Tools[i].Hash = snapshots.ToolFingerprint(b.Tools[i])
		}
	}

	// Add a new tool.
	added := snapshots.ToolInfo{
		NamespacedName:   "linear.create_issue",
		ServerID:         "linear",
		Description:      "Create a Linear issue",
		InputSchema:      json.RawMessage(`{"type":"object"}`),
		RiskClass:        "medium",
		RequiresApproval: true,
	}
	added.Hash = snapshots.ToolFingerprint(added)
	b.Tools = append(b.Tools, added)

	d := snapshots.DiffSnapshots(a, b)
	if d == nil || d.IsEmpty() {
		t.Fatal("expected non-empty diff")
	}
	if got, want := d.Tools.Added, []string{"linear.create_issue"}; !equalStrings(got, want) {
		t.Errorf("Added = %v, want %v", got, want)
	}
	if got, want := d.Tools.Removed, []string{"github.delete_repo"}; !equalStrings(got, want) {
		t.Errorf("Removed = %v, want %v", got, want)
	}
	if len(d.Tools.Modified) != 1 {
		t.Fatalf("expected 1 modified tool, got %d", len(d.Tools.Modified))
	}
	mod := d.Tools.Modified[0]
	if mod.Name != "github.create_issue" {
		t.Errorf("Modified.Name = %q, want github.create_issue", mod.Name)
	}
	if mod.OldHash == mod.NewHash {
		t.Error("Modified entry old/new hashes should differ")
	}
	if !containsString(mod.FieldsChanged, "description") {
		t.Errorf("FieldsChanged must include description, got %v", mod.FieldsChanged)
	}
}

func TestDiffSnapshots_NoChange_EmptyDiff(t *testing.T) {
	t.Parallel()
	a := sampleSnapshot()
	d := snapshots.DiffSnapshots(a, a)
	if d == nil {
		t.Fatal("DiffSnapshots returned nil")
	}
	if !d.IsEmpty() {
		t.Errorf("identical snapshots should produce an empty diff, got %+v", d)
	}
}

func TestDiffSnapshots_ResourcesPromptsSkills_AddedRemoved(t *testing.T) {
	t.Parallel()
	a := sampleSnapshot()
	b := sampleSnapshot()

	// Resources: drop one, add one.
	b.Resources = []snapshots.ResourceInfo{
		{URI: "github://repo/x", UpstreamURI: "x", ServerID: "github"},
		{URI: "github://repo/y", UpstreamURI: "y", ServerID: "github"},
	}

	// Prompts: drop one, add one.
	b.Prompts = []snapshots.PromptInfo{
		{NamespacedName: "github.summarise", ServerID: "github"},
		{NamespacedName: "github.refactor", ServerID: "github"},
	}

	// Skills: drop one, add one.
	b.Skills = []snapshots.SkillInfo{
		{ID: "skill_a", Version: "1.0.0", EnabledForSession: true},
		{ID: "skill_c", Version: "0.1.0", EnabledForSession: true},
	}

	d := snapshots.DiffSnapshots(a, b)
	if d == nil || d.IsEmpty() {
		t.Fatal("expected non-empty diff")
	}
	if !equalStrings(d.Resources.Added, []string{"github://repo/y"}) {
		t.Errorf("Resources.Added = %v", d.Resources.Added)
	}
	if !equalStrings(d.Resources.Removed, []string{"linear://issue/1"}) {
		t.Errorf("Resources.Removed = %v", d.Resources.Removed)
	}
	if !equalStrings(d.Prompts.Added, []string{"github.refactor"}) {
		t.Errorf("Prompts.Added = %v", d.Prompts.Added)
	}
	if !equalStrings(d.Prompts.Removed, []string{"linear.weekly"}) {
		t.Errorf("Prompts.Removed = %v", d.Prompts.Removed)
	}
	if !equalStrings(d.Skills.Added, []string{"skill_c"}) {
		t.Errorf("Skills.Added = %v", d.Skills.Added)
	}
	if !equalStrings(d.Skills.Removed, []string{"skill_b"}) {
		t.Errorf("Skills.Removed = %v", d.Skills.Removed)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
