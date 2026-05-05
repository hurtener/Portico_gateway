package protocol

// Method names — the single source of truth. Production code references the
// constants; raw "tools/list" string literals are forbidden by AGENTS.md §8.
const (
	MethodInitialize             = "initialize"
	NotifInitialized             = "notifications/initialized"
	MethodPing                   = "ping"
	MethodToolsList              = "tools/list"
	MethodToolsCall              = "tools/call"
	MethodResourcesList          = "resources/list"
	MethodResourcesRead          = "resources/read"
	MethodResourcesTemplatesList = "resources/templates/list"
	MethodPromptsList            = "prompts/list"
	MethodPromptsGet             = "prompts/get"

	NotifCancelled            = "notifications/cancelled"
	NotifProgress             = "notifications/progress"
	NotifToolsListChanged     = "notifications/tools/list_changed"
	NotifResourcesListChanged = "notifications/resources/list_changed"
	NotifResourcesUpdated     = "notifications/resources/updated"
	NotifPromptsListChanged   = "notifications/prompts/list_changed"
)
