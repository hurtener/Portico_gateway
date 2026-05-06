// Package manifest defines the on-disk Skill Pack manifest schema
// (skills/v1) plus YAML/JSON marshaling. The schema lives in
// schema.json and is enforced via the loader package; this file holds
// only the typed shapes.
package manifest

// SpecVersion is the literal value Skill Pack manifests must declare in
// `spec`. Bumping this is a breaking change for every checked-in pack
// and requires a migration plan.
const SpecVersion = "skills/v1"

// Manifest is the parsed representation of a Skill Pack's manifest.yaml.
type Manifest struct {
	ID           string   `yaml:"id" json:"id"`
	Title        string   `yaml:"title" json:"title"`
	Version      string   `yaml:"version" json:"version"`
	Spec         string   `yaml:"spec" json:"spec"`
	Description  string   `yaml:"description,omitempty" json:"description,omitempty"`
	Instructions string   `yaml:"instructions" json:"instructions"`
	Resources    []string `yaml:"resources,omitempty" json:"resources,omitempty"`
	Prompts      []string `yaml:"prompts,omitempty" json:"prompts,omitempty"`
	Binding      Binding  `yaml:"binding" json:"binding"`
}

// Binding describes how a skill plugs into the live registry: which
// servers it depends on, which tools it uses, what UI it surfaces, and
// any policy hints.
type Binding struct {
	ServerDependencies []string     `yaml:"server_dependencies,omitempty" json:"server_dependencies,omitempty"`
	RequiredTools      []string     `yaml:"required_tools,omitempty" json:"required_tools,omitempty"`
	OptionalTools      []string     `yaml:"optional_tools,omitempty" json:"optional_tools,omitempty"`
	Policy             Policy       `yaml:"policy,omitempty" json:"policy,omitempty"`
	UI                 *UIBinding   `yaml:"ui,omitempty" json:"ui,omitempty"`
	Entitlements       Entitlements `yaml:"entitlements,omitempty" json:"entitlements,omitempty"`
}

// Policy is the skill's suggestion for risk classification + approval
// requirements. Phase 4 records these values; Phase 5 wires them to
// the policy engine + approval flow.
type Policy struct {
	RequiresApproval []string          `yaml:"requires_approval,omitempty" json:"requires_approval,omitempty"`
	RiskClasses      map[string]string `yaml:"risk_classes,omitempty" json:"risk_classes,omitempty"`
}

// UIBinding declares the optional MCP App resource the skill exposes.
type UIBinding struct {
	ResourceURI string `yaml:"resource_uri" json:"resource_uri"`
}

// Entitlements gates a skill behind tenant plan tiers.
type Entitlements struct {
	Plans []string `yaml:"plans,omitempty" json:"plans,omitempty"`
}

// AllTools returns the union of required + optional tools — useful for
// dependency checks and audit decoration.
func (m *Manifest) AllTools() []string {
	out := make([]string, 0, len(m.Binding.RequiredTools)+len(m.Binding.OptionalTools))
	out = append(out, m.Binding.RequiredTools...)
	out = append(out, m.Binding.OptionalTools...)
	return out
}
