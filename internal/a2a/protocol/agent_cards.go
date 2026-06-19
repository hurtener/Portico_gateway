package protocol

// AgentProvider identifies the organisation responsible for an agent.
// Used in the `provider` field of AgentCard when an operator wants to
// surface who built the agent.
type AgentProvider struct {
	// Organization is the human-readable provider name.
	Organization string `json:"organization"`
	// URL is a contact URL for the provider.
	URL string `json:"url"`
}

// AgentSkill describes a single skill an agent exposes. Skills are the
// A2A analog of MCP tools' coarse-grained categorisation: they group
// tasks under a named capability surface.
type AgentSkill struct {
	// ID is the stable, unique skill identifier within the card.
	ID string `json:"id"`
	// Name is the human-readable skill name.
	Name string `json:"name"`
	// Description is a human-readable explanation of what the skill
	// offers.
	Description string `json:"description"`
	// Tags are free-form labels an operator can use to group skills in
	// UIs.
	Tags []string `json:"tags,omitempty"`
	// Examples are example prompts or inputs that exercise the skill.
	Examples []string `json:"examples,omitempty"`
	// InputModes are the content/MIME types the skill accepts as input.
	InputModes []string `json:"inputModes,omitempty"`
	// OutputModes are the content/MIME types the skill produces as
	// output.
	OutputModes []string `json:"outputModes,omitempty"`
}

// AgentCard is the A2A discovery document, served at the well-known path
// (typically `/.well-known/agent.json`). Every A2A agent publishes one.
// It is the A2A analog of the MCP initialize response: enough
// information for a client to decide whether and how to talk to the
// agent.
//
// SecuritySchemes / Security fields are deliberately omitted here — they
// land in a later unit once the auth story is firmed up.
type AgentCard struct {
	// Name is the agent's human-readable name.
	Name string `json:"name"`
	// Description is a short summary of what the agent does.
	Description string `json:"description,omitempty"`
	// URL is the agent's A2A endpoint that clients POST JSON-RPC to.
	URL string `json:"url"`
	// Version is the agent's own version string (independent of the
	// protocol version).
	Version string `json:"version"`
	// ProtocolVersion is the A2A protocol revision the agent speaks.
	// Operators should set this to protocol.SpecVersion.
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	// Provider identifies the organisation behind the agent.
	Provider *AgentProvider `json:"provider,omitempty"`
	// Capabilities advertises optional A2A capabilities the agent
	// implements.
	Capabilities AgentCapabilities `json:"capabilities"`
	// DefaultInputModes are the content types the agent accepts when no
	// per-message override is given.
	DefaultInputModes []string `json:"defaultInputModes,omitempty"`
	// DefaultOutputModes are the content types the agent produces when
	// no per-message override is given.
	DefaultOutputModes []string `json:"defaultOutputModes,omitempty"`
	// Skills are the agent's discoverable skills.
	Skills []AgentSkill `json:"skills,omitempty"`
	// DocumentationURL optionally points at longer-form docs.
	DocumentationURL string `json:"documentationUrl,omitempty"`
	// SupportsAuthenticatedExtendedCard indicates the agent will return
	// an extended agent card when called via
	// `agent/getAuthenticatedExtendedCard`.
	SupportsAuthenticatedExtendedCard bool `json:"supportsAuthenticatedExtendedCard,omitempty"`
}
