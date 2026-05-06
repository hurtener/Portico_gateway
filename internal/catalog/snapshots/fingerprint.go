package snapshots

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
)

// Fingerprinting policy:
// Every hash is sha256 over canonical JSON. Canonical JSON is:
//   - object keys in sorted order
//   - no insignificant whitespace
//   - null values omitted from objects (so {"x": null} ≡ {})
//   - lists kept in input order (callers sort by stable key first)
//
// Subagent fills the implementation of canonicalJSON below; this file
// owns the public API and the high-level helpers.

// ToolFingerprint hashes one tool's user-visible shape: namespaced name,
// server, description, schema, annotations, resolved risk class, approval
// requirement, owning skill (if any). Re-running on the same input
// returns the same hex digest.
func ToolFingerprint(t ToolInfo) string {
	cj, _ := canonicalJSON(map[string]any{
		"namespaced_name":   t.NamespacedName,
		"server_id":         t.ServerID,
		"description":       t.Description,
		"input_schema":      rawJSONOrNil(t.InputSchema),
		"annotations":       t.Annotations,
		"risk_class":        t.RiskClass,
		"requires_approval": t.RequiresApproval,
		"skill_id":          t.SkillID,
	})
	sum := sha256.Sum256(cj)
	return hex.EncodeToString(sum[:])
}

// ServerToolsFingerprint hashes the per-server tool list ordered by name.
// Used by the drift detector — two upstream tool-lists with the same
// shape produce the same hash regardless of upstream ordering.
func ServerToolsFingerprint(tools []protocol.Tool) string {
	cp := append([]protocol.Tool(nil), tools...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Name < cp[j].Name })
	cj, _ := canonicalJSON(cp)
	sum := sha256.Sum256(cj)
	return hex.EncodeToString(sum[:])
}

// ResourcesFingerprint hashes the namespaced resource list ordered by URI.
func ResourcesFingerprint(rs []ResourceInfo) string {
	cp := append([]ResourceInfo(nil), rs...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].URI < cp[j].URI })
	cj, _ := canonicalJSON(cp)
	sum := sha256.Sum256(cj)
	return hex.EncodeToString(sum[:])
}

// PromptsFingerprint hashes the namespaced prompt list ordered by name.
func PromptsFingerprint(ps []PromptInfo) string {
	cp := append([]PromptInfo(nil), ps...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].NamespacedName < cp[j].NamespacedName })
	cj, _ := canonicalJSON(cp)
	sum := sha256.Sum256(cj)
	return hex.EncodeToString(sum[:])
}

// OverallFingerprint hashes the full snapshot's sub-fingerprints. The
// snapshot's own ID + CreatedAt are deliberately excluded so two
// independently-built snapshots over identical state hash equal.
func OverallFingerprint(s *Snapshot) string {
	if s == nil {
		return ""
	}
	// Tool hashes already encode the fully-resolved view. Order by
	// namespaced name so reordering doesn't move the overall hash.
	tHashes := make([]string, 0, len(s.Tools))
	cp := append([]ToolInfo(nil), s.Tools...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].NamespacedName < cp[j].NamespacedName })
	for _, t := range cp {
		tHashes = append(tHashes, t.NamespacedName+":"+t.Hash)
	}
	srv := append([]ServerInfo(nil), s.Servers...)
	sort.Slice(srv, func(i, j int) bool { return srv[i].ID < srv[j].ID })
	cj, _ := canonicalJSON(map[string]any{
		"servers":        srv,
		"tool_hashes":    tHashes,
		"resources_hash": ResourcesFingerprint(s.Resources),
		"prompts_hash":   PromptsFingerprint(s.Prompts),
		"skills":         sortedSkills(s.Skills),
		"policies":       s.Policies,
		"credentials":    sortedCreds(s.Credentials),
	})
	sum := sha256.Sum256(cj)
	return hex.EncodeToString(sum[:])
}

func sortedSkills(in []SkillInfo) []SkillInfo {
	out := append([]SkillInfo(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sortedCreds(in []CredentialInfo) []CredentialInfo {
	out := append([]CredentialInfo(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].ServerID < out[j].ServerID })
	return out
}

// rawJSONOrNil treats a zero-length json.RawMessage as missing so the
// canonicaliser drops it (matching the "omit null" rule).
func rawJSONOrNil(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// canonicalJSON is the deterministic-JSON marshaller. Subagent fills the
// body. Returns (bytes, error). Empty input returns ("null", nil).
//
// Rules:
//   - map[string]any: keys sorted ascending; nil values dropped; recurse on values.
//   - []any / []T: keep input order; recurse.
//   - structs: marshal via encoding/json then re-encode through this path
//     so the canonicalisation rules apply uniformly (the easy way is to
//     json.Marshal then json.Unmarshal into any, then canonicalise).
//   - json.RawMessage: parsed into any first, then canonicalised.
//   - basic types: string/int/float/bool — encoded with strconv-style
//     deterministic representations.
//
// Determinism is non-negotiable: the same logical value must always
// produce the same bytes regardless of map iteration order.
func canonicalJSON(v any) ([]byte, error) {
	return canonicalEncode(v)
}

// DiffSnapshots returns the structured difference (a → b). Used by the
// drift detector to render structured diff payloads and by the REST API
// to render the snapshot diff endpoint.
func DiffSnapshots(a, b *Snapshot) *Diff {
	d := &Diff{}
	if a == nil || b == nil {
		return d
	}
	// Tools.
	aTools := indexToolsByName(a.Tools)
	bTools := indexToolsByName(b.Tools)
	for name := range bTools {
		if _, ok := aTools[name]; !ok {
			d.Tools.Added = append(d.Tools.Added, name)
		}
	}
	for name, oldTool := range aTools {
		newTool, ok := bTools[name]
		if !ok {
			d.Tools.Removed = append(d.Tools.Removed, name)
			continue
		}
		if oldTool.Hash != newTool.Hash {
			d.Tools.Modified = append(d.Tools.Modified, ModifiedTool{
				Name:          name,
				FieldsChanged: changedFields(oldTool, newTool),
				OldHash:       oldTool.Hash,
				NewHash:       newTool.Hash,
			})
		}
	}
	sort.Strings(d.Tools.Added)
	sort.Strings(d.Tools.Removed)
	sort.Slice(d.Tools.Modified, func(i, j int) bool { return d.Tools.Modified[i].Name < d.Tools.Modified[j].Name })

	// Resources.
	aURI := setOf(a.Resources, func(r ResourceInfo) string { return r.URI })
	bURI := setOf(b.Resources, func(r ResourceInfo) string { return r.URI })
	d.Resources.Added = sortedDiff(bURI, aURI)
	d.Resources.Removed = sortedDiff(aURI, bURI)

	// Prompts.
	aP := setOf(a.Prompts, func(p PromptInfo) string { return p.NamespacedName })
	bP := setOf(b.Prompts, func(p PromptInfo) string { return p.NamespacedName })
	d.Prompts.Added = sortedDiff(bP, aP)
	d.Prompts.Removed = sortedDiff(aP, bP)

	// Skills.
	aS := setOf(a.Skills, func(s SkillInfo) string { return s.ID })
	bS := setOf(b.Skills, func(s SkillInfo) string { return s.ID })
	d.Skills.Added = sortedDiff(bS, aS)
	d.Skills.Removed = sortedDiff(aS, bS)
	return d
}

func indexToolsByName(in []ToolInfo) map[string]ToolInfo {
	out := make(map[string]ToolInfo, len(in))
	for _, t := range in {
		out[t.NamespacedName] = t
	}
	return out
}

func setOf[T any](in []T, key func(T) string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, v := range in {
		out[key(v)] = struct{}{}
	}
	return out
}

func sortedDiff(a, b map[string]struct{}) []string {
	var out []string
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func changedFields(a, b ToolInfo) []string {
	out := make([]string, 0, 4)
	if string(a.InputSchema) != string(b.InputSchema) {
		out = append(out, "input_schema")
	}
	if a.Description != b.Description {
		out = append(out, "description")
	}
	if !equalAnnotations(a.Annotations, b.Annotations) {
		out = append(out, "annotations")
	}
	if a.RiskClass != b.RiskClass {
		out = append(out, "risk_class")
	}
	if a.RequiresApproval != b.RequiresApproval {
		out = append(out, "requires_approval")
	}
	if a.SkillID != b.SkillID {
		out = append(out, "skill_id")
	}
	return out
}

func equalAnnotations(a, b *protocol.ToolAnnotations) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	ja, _ := canonicalJSON(a)
	jb, _ := canonicalJSON(b)
	return string(ja) == string(jb)
}

// newSnapshotID returns a sortable id (time-prefixed). Phase 6 uses a
// custom encoding rather than the audit ULID dep so this package stays
// dep-free if it's ever extracted.
func newSnapshotID(t time.Time) string {
	var b [16]byte
	//nolint:gosec // UnixNano is positive in practice; cast is safe through 2262.
	binary.BigEndian.PutUint64(b[:8], uint64(t.UnixNano()))
	if _, err := rand.Read(b[8:]); err != nil {
		// Time-only fallback; the random suffix is for collision safety,
		// not security.
		for i := 8; i < 16; i++ {
			b[i] = byte(t.UnixNano() >> ((i - 8) * 8))
		}
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
	return "snap_" + enc
}

// Compile-time guard against unused imports if the implementation moves.
var _ = fmt.Sprintf
