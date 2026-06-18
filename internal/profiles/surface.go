package profiles

// LiveCatalog is the live, tenant-scoped inventory a profile's surface is
// intersected against. The caller (the /surface REST handler) gathers it from
// the registry, the skills catalog, and the LLM model store at request time, so
// the materialised surface always reflects current state — a server registered
// after the profile was created but matching its allowlist appears immediately.
type LiveCatalog struct {
	Servers []string // live registered MCP server ids for the tenant
	Skills  []string // live Skill Pack ids
	Aliases []string // live LLM model aliases for the tenant
}

// Surface is the materialised inventory a profile currently sees: its
// allowlists intersected with the live catalog. Tools are the profile's
// declared finer-grain allowlist restricted to live + allowed servers; an empty
// declared list means "all tools in the allowed servers" (a session-time
// expansion), so the materialised tool list is empty in that case.
type Surface struct {
	ProfileID string   `json:"profile_id"`
	IsDefault bool     `json:"is_default"`
	Servers   []string `json:"servers"`
	Tools     []string `json:"tools"`
	Skills    []string `json:"skills"`
	Models    []string `json:"models"`
}

// Materialize intersects the profile's allowlists with the live catalog. The
// default profile (no restriction) materialises to the full live catalog.
func (p *Profile) Materialize(live LiveCatalog) Surface {
	out := Surface{
		Servers: filterAllowed(live.Servers, p.AllowsServer),
		Skills:  filterAllowed(live.Skills, p.AllowsSkill),
		Models:  filterAllowed(live.Aliases, p.AllowsAlias),
		Tools:   p.materializeTools(),
	}
	if p != nil {
		out.ProfileID = p.ID
		out.IsDefault = p.IsDefault
	}
	return out
}

// materializeTools returns the profile's declared tool allowlist restricted to
// tools whose server is itself allowed. An empty declared allowlist yields an
// empty list ("all tools in the allowed servers" is resolved per session, not
// here). The default profile declares no tools, so it yields an empty list too.
func (p *Profile) materializeTools() []string {
	if p == nil || len(p.AllowedTools) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(p.AllowedTools))
	for _, t := range p.AllowedTools {
		if p.AllowsTool(t) {
			out = append(out, t)
		}
	}
	return out
}

func filterAllowed(items []string, allow func(string) bool) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if allow(it) {
			out = append(out, it)
		}
	}
	return out
}
