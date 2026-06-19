package protocol

// Method names — the single source of truth. Production code references
// these constants; raw "message/send" string literals are forbidden by
// AGENTS.md §8.
const (
	// Message-method half of the A2A spec: clients send a message to an
	// agent; the agent dispatches to a task and returns either a sync
	// result or a streaming channel.
	MethodMessageSend   = "message/send"
	MethodMessageStream = "message/stream"

	// Task-method half: lookup, cancel, or resubscribe to an existing
	// task.
	MethodTasksGet         = "tasks/get"
	MethodTasksCancel      = "tasks/cancel"
	MethodTasksResubscribe = "tasks/resubscribe"

	// Push-notification-config methods: register a webhook target for
	// long-running task updates.
	MethodTasksPushNotificationConfigSet    = "tasks/pushNotificationConfig/set"
	MethodTasksPushNotificationConfigGet    = "tasks/pushNotificationConfig/get"
	MethodTasksPushNotificationConfigList   = "tasks/pushNotificationConfig/list"
	MethodTasksPushNotificationConfigDelete = "tasks/pushNotificationConfig/delete"

	// Authenticated-extended-card method: returns an agent card variant
	// populated with additional fields visible only to authenticated
	// callers.
	MethodAgentGetAuthenticatedExtendedCard = "agent/getAuthenticatedExtendedCard"
)
