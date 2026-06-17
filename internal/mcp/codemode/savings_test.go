package codemode

import (
	"encoding/json"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/catalog/snapshots"
)

func bigSnapshot(n int) *snapshots.Snapshot {
	snap := &snapshots.Snapshot{ID: "s", TenantID: "t"}
	for i := 0; i < n; i++ {
		snap.Tools = append(snap.Tools, snapshots.ToolInfo{
			NamespacedName: "server.tool_with_a_reasonably_long_name",
			Description:    "A tool that does a thing and has a moderately long description for realism.",
			InputSchema:    json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"integer"}},"required":["a"]}`),
		})
	}
	return snap
}

func TestSavings_DeterministicForFixedSnapshot(t *testing.T) {
	snap := bigSnapshot(50)
	a := EstimateTokensSaved(snap, 3, 200, 400)
	b := EstimateTokensSaved(snap, 3, 200, 400)
	if a != b {
		t.Fatalf("non-deterministic: %d vs %d", a, b)
	}
}

func TestSavings_LargeCatalogSavesTokens(t *testing.T) {
	// A 50-tool snapshot that would ship its whole catalog in plain mode yields
	// substantial savings against a small Code Mode execution.
	snap := bigSnapshot(50)
	saved := EstimateTokensSaved(snap, 2, 120, 240)
	if saved <= 0 {
		t.Fatalf("expected positive savings for a 50-tool catalog, got %d", saved)
	}
	// Sanity: within 10% of the analytical answer (acceptance #11).
	catalog := CatalogRenderTokens(snap)
	want := (catalog - metaToolsRenderChars/charsPerToken) + 2*(perCallOverheadChars/charsPerToken) - (120+240)/charsPerToken
	if saved != want {
		t.Errorf("savings = %d, analytical = %d", saved, want)
	}
}

func TestSavings_ClampedAtZero(t *testing.T) {
	// Tiny snapshot, huge executed code: no savings, never negative.
	snap := bigSnapshot(1)
	saved := EstimateTokensSaved(snap, 0, 100000, 0)
	if saved != 0 {
		t.Fatalf("expected clamp to 0, got %d", saved)
	}
}

func TestSavings_NilSnapshot(t *testing.T) {
	if got := CatalogRenderTokens(nil); got != 0 {
		t.Errorf("nil snapshot catalog tokens = %d, want 0", got)
	}
	if got := EstimateTokensSaved(nil, 0, 0, 0); got != 0 {
		t.Errorf("nil snapshot savings = %d, want 0", got)
	}
}

func TestSavings_MoreToolCallsMoreSavings(t *testing.T) {
	snap := bigSnapshot(20)
	a := EstimateTokensSaved(snap, 1, 100, 100)
	b := EstimateTokensSaved(snap, 5, 100, 100)
	if b <= a {
		t.Errorf("more tool calls should save more: 1-call=%d 5-call=%d", a, b)
	}
}
