package protocol

// AgentCapabilities advertises what an A2A agent supports above the
// minimum required surface. Each field corresponds to one A2A capability
// flag; omit a field (or leave it false) to indicate the agent does not
// advertise that capability.
type AgentCapabilities struct {
	// Streaming indicates the agent supports `message/stream` (SSE-style
	// server-initiated updates for long-running tasks).
	Streaming bool `json:"streaming,omitempty"`

	// PushNotifications indicates the agent accepts
	// `tasks/pushNotificationConfig/*` webhooks for asynchronous task
	// updates.
	PushNotifications bool `json:"pushNotifications,omitempty"`

	// StateTransitionHistory indicates the agent will return the full
	// task state history in TaskStatus responses rather than only the
	// current state.
	StateTransitionHistory bool `json:"stateTransitionHistory,omitempty"`
}
