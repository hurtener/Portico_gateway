package mcpgw

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/hurtener/Portico_gateway/internal/mcp/protocol"
	"github.com/hurtener/Portico_gateway/internal/profiles"
)

// Agent Profile enforcement for the Skills surface (Phase 14, acceptance #8). A
// Skill not in the request profile's allowed_skills must not appear in
// prompts/list or as a skill:// resource, and must not be reachable via
// prompts/get or resources/read. The profile is resolved into the request
// context by the profile middleware; the aggregators read it via
// profiles.FromContext. A nil/default profile is a no-op (back-compat), so the
// skills runtime itself stays profile-unaware — exactly as the registry does
// for tool filtering.

// skillIDFromResourceURI extracts the owning skill id from a skill:// resource
// URI. skill://{ns}/{name}/{rel} → "{ns}.{name}". The synthetic skill://_index
// has no owning skill (returns "", false) and is filtered by body instead.
func skillIDFromResourceURI(uri string) (string, bool) {
	const prefix = "skill://"
	if !strings.HasPrefix(uri, prefix) {
		return "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(uri, prefix), "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return parts[0] + "." + parts[1], true
}

// skillIDFromPromptName extracts the owning skill id from a namespaced skill
// prompt name. Skill ids are themselves dotted ("{ns}.{name}"), so the skill id
// is the first two dot-separated segments; the remainder is the prompt name.
// A name with fewer than two dots (a plain "{server}.{prompt}") is not a skill
// prompt and returns ("", false).
func skillIDFromPromptName(name string) (string, bool) {
	first := strings.IndexByte(name, '.')
	if first <= 0 {
		return "", false
	}
	rest := name[first+1:]
	second := strings.IndexByte(rest, '.')
	if second <= 0 {
		return "", false
	}
	return name[:first+1+second], true
}

// filterSkillPromptsByProfile drops skill prompts the request profile does not
// allow. A nil/default profile returns the slice unchanged.
func filterSkillPromptsByProfile(ctx context.Context, prompts []protocol.Prompt) []protocol.Prompt {
	prof := profiles.FromContext(ctx)
	if prof == nil || prof.IsDefault {
		return prompts
	}
	out := make([]protocol.Prompt, 0, len(prompts))
	for _, p := range prompts {
		if id, ok := skillIDFromPromptName(p.Name); ok && !prof.AllowsSkill(id) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// filterSkillResourcesByProfile drops skill:// resources the request profile
// does not allow. The synthetic skill://_index is retained (its body is filtered
// separately by filterSkillIndexBody). A nil/default profile is a no-op.
func filterSkillResourcesByProfile(ctx context.Context, res []protocol.Resource) []protocol.Resource {
	prof := profiles.FromContext(ctx)
	if prof == nil || prof.IsDefault {
		return res
	}
	out := make([]protocol.Resource, 0, len(res))
	for _, r := range res {
		if r.URI == "skill://_index" {
			out = append(out, r)
			continue
		}
		if id, ok := skillIDFromResourceURI(r.URI); ok && !prof.AllowsSkill(id) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// skillResourceViolation builds the typed error returned when a resources/read
// targets a skill:// resource outside the caller's profile surface.
func skillResourceViolation(profileID, skillID, uri string) *protocol.Error {
	return protocol.NewError(protocol.ErrAgentProfileViolation, "agent profile violation", map[string]any{
		"profile_id": profileID,
		"skill":      skillID,
		"uri":        uri,
		"reason":     "skill_outside_profile",
	})
}

// skillPromptViolation builds the typed error returned when a prompts/get
// targets a skill prompt outside the caller's profile surface.
func skillPromptViolation(profileID, skillID, prompt string) *protocol.Error {
	return protocol.NewError(protocol.ErrAgentProfileViolation, "agent profile violation", map[string]any{
		"profile_id": profileID,
		"skill":      skillID,
		"prompt":     prompt,
		"reason":     "skill_outside_profile",
	})
}

// filterSkillIndexBody rewrites a skill://_index JSON body to omit skills the
// request profile does not allow, so the catalog the agent sees matches its
// surface. A nil/default profile returns the body unchanged. On any parse
// failure it returns the body untouched (the index is best-effort metadata, not
// an authorization boundary — the boundary is the per-resource filter above).
func filterSkillIndexBody(ctx context.Context, body []byte) []byte {
	prof := profiles.FromContext(ctx)
	if prof == nil || prof.IsDefault {
		return body
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return body
	}
	raw, ok := doc["skills"]
	if !ok {
		return body
	}
	var skills []json.RawMessage
	if err := json.Unmarshal(raw, &skills); err != nil {
		return body
	}
	kept := make([]json.RawMessage, 0, len(skills))
	for _, s := range skills {
		var meta struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(s, &meta); err != nil {
			continue
		}
		if prof.AllowsSkill(meta.ID) {
			kept = append(kept, s)
		}
	}
	keptRaw, err := json.Marshal(kept)
	if err != nil {
		return body
	}
	doc["skills"] = keptRaw
	out, err := json.Marshal(doc)
	if err != nil {
		return body
	}
	return out
}
