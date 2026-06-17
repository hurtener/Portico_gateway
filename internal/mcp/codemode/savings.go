// Package codemode holds the Code Mode token-savings estimator and shared
// types that are not specific to the catalog projector or the Starlark runtime.
package codemode

import "github.com/hurtener/Portico_gateway/internal/catalog/snapshots"

// The estimator approximates the token spend Code Mode avoids versus a plain
// session that ships the full tool catalog in every turn. It is deterministic
// (a fixed function of its inputs) and documented in
// docs/concepts/code-mode-savings.md; the unit tests assert byte-stable output
// for fixed snapshots (acceptance #11).
const (
	// charsPerToken is the BPE-shaped approximation used throughout (≈4 chars
	// per token on English text + JSON).
	charsPerToken = 4

	// perToolFramingChars approximates the JSON envelope around each tool
	// definition in an OpenAI tools blob (braces, keys, quoting) beyond the
	// tool's own name/description/schema bytes.
	perToolFramingChars = 40

	// perCallOverheadChars approximates the per-round-trip "model framing"
	// overhead a plain session pays for each separate tool call (the assistant
	// turn that emits the call plus the tool-result turn) — savings Code Mode
	// captures by batching calls inside one execution. Measured against the
	// Phase 11 replay corpus.
	perCallOverheadChars = 320

	// metaToolsRenderChars approximates the rendered size of the four meta-tool
	// definitions a Code Mode session sees instead of the catalog.
	metaToolsRenderChars = 900
)

// EstimateTokensSaved returns the estimated tokens Code Mode saved for one
// execution against a hypothetical plain-mode turn over the same snapshot.
//
//	saved = (catalog_render - meta_tools_render)        // catalog never shipped
//	      + num_tool_calls * per_call_overhead          // round trips collapsed
//	      - (code + results)                            // cost Code Mode adds
//
// The result is clamped at zero: a tiny snapshot where the executed code costs
// more than the catalog it replaced yields no savings, never a negative number.
func EstimateTokensSaved(snap *snapshots.Snapshot, numToolCalls, codeBytes, resultBytes int) int {
	catalogTokens := CatalogRenderTokens(snap)
	metaTokens := metaToolsRenderChars / charsPerToken
	callTokens := numToolCalls * (perCallOverheadChars / charsPerToken)
	execTokens := (codeBytes + resultBytes) / charsPerToken

	saved := (catalogTokens - metaTokens) + callTokens - execTokens
	if saved < 0 {
		return 0
	}
	return saved
}

// CatalogRenderTokens estimates the token cost of rendering the snapshot's full
// tool catalog as an OpenAI tool-definitions blob — the spend a plain session
// pays every turn and Code Mode avoids. Deterministic for a fixed snapshot.
func CatalogRenderTokens(snap *snapshots.Snapshot) int {
	if snap == nil {
		return 0
	}
	chars := 0
	for _, ti := range snap.Tools {
		chars += len(ti.NamespacedName) + len(ti.Description) + len(ti.InputSchema) + perToolFramingChars
	}
	return chars / charsPerToken
}
