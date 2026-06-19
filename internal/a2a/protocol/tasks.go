package protocol

// Wire-type discipline for the task/message half of the A2A spec.
//
// The JSON-RPC envelope, method-name constants, error codes, capabilities,
// and agent-card types already live in this package (see version.go,
// types.go, methods.go, errors.go, capabilities.go, agent_cards.go). This
// file is the single source of truth for the Task / Message / Part /
// Artifact wire shapes used by the message/* and tasks/* methods. It is
// pure types — no I/O, no logic — and other packages import these symbols
// but never redefine them (AGENTS.md §13).

// Well-known "kind" discriminators that ride along on the wire.
const (
	// KindMessage is the value of Message.Kind on the wire.
	KindMessage = "message"
	// KindTask is the value of Task.Kind on the wire.
	KindTask = "task"
)

// Part-kind discriminators used by Part.Kind on the wire.
const (
	PartKindText = "text"
	PartKindFile = "file"
	PartKindData = "data"
)

// Role enumerates the two parties that author a Message. A2A's wire form
// is the bare lowercase string.
const (
	RoleUser  = "user"
	RoleAgent = "agent"
)

// TaskState is the lifecycle state of an A2A task. The wire values are
// fixed by the A2A spec — note the American spelling "canceled" and the
// hyphenated multi-word states ("input-required", "auth-required").
type TaskState string

// Task lifecycle states. The string values are part of the wire format
// and must match the A2A spec exactly; do NOT rename without an RFC.
const (
	TaskStateSubmitted     TaskState = "submitted"
	TaskStateWorking       TaskState = "working"
	TaskStateInputRequired TaskState = "input-required"
	TaskStateCompleted     TaskState = "completed"
	TaskStateCanceled      TaskState = "canceled"
	TaskStateFailed        TaskState = "failed"
	TaskStateRejected      TaskState = "rejected"
	TaskStateAuthRequired  TaskState = "auth-required"
	TaskStateUnknown       TaskState = "unknown"
)

// FileContent is a file part payload carried inside a Part of Kind
// "file". Exactly one of Bytes or URI is set on the wire: either the
// inline base64-encoded bytes, or a URI to fetch them from.
type FileContent struct {
	// Name is the file's human-readable name.
	Name string `json:"name,omitempty"`
	// MimeType is the MIME type of the file's contents (e.g.
	// "image/png", "application/pdf").
	MimeType string `json:"mimeType,omitempty"`
	// Bytes is the base64-encoded file contents. XOR with URI.
	Bytes string `json:"bytes,omitempty"`
	// URI is the location from which the file contents can be fetched.
	// XOR with Bytes.
	URI string `json:"uri,omitempty"`
}

// Part is one piece of a Message or Artifact. A2A's part shapes —
// text, file, and structured data — are modelled as a single flat
// discriminated union with a Kind field, mirroring the convention used
// by the MCP package's ContentBlock (see internal/mcp/protocol/types.go).
// No custom marshal/unmarshal, no Go interface union: the kind string
// on the wire is the discriminator; unused fields stay zero.
//
// Kind takes one of PartKindText, PartKindFile, PartKindData.
type Part struct {
	// Kind discriminates which of the optional fields below is set:
	// "text", "file", or "data".
	Kind string `json:"kind"`
	// Text is the body of a text part (Kind == "text").
	Text string `json:"text,omitempty"`
	// File is the body of a file part (Kind == "file").
	File *FileContent `json:"file,omitempty"`
	// Data is the body of a data part (Kind == "data") — arbitrary
	// JSON-shaped payload.
	Data map[string]any `json:"data,omitempty"`
	// Metadata is optional, part-level metadata (free-form).
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Message is one turn in a task's conversation. A message carries one
// or more Parts and is authored by either the user or the agent.
type Message struct {
	// Role is one of RoleUser or RoleAgent.
	Role string `json:"role"`
	// Parts are the contents of this message, in order.
	Parts []Part `json:"parts"`
	// MessageID is the unique identifier of this message.
	MessageID string `json:"messageId"`
	// ContextID groups messages across a multi-turn task.
	ContextID string `json:"contextId,omitempty"`
	// TaskID references the task this message belongs to. Omitted on
	// the initial send; populated on subsequent turns.
	TaskID string `json:"taskId,omitempty"`
	// ReferenceTaskIDs are task IDs this message cites (e.g. a prior
	// task whose output informs this turn).
	ReferenceTaskIDs []string `json:"referenceTaskIds,omitempty"`
	// Metadata is optional, message-level metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
	// Kind is the on-the-wire discriminator; it is always KindMessage
	// when present.
	Kind string `json:"kind,omitempty"`
}

// Artifact is an output artifact produced by a task — a named bundle of
// Parts the agent hands back to the caller once the task completes (or
// streams for long-running tasks).
type Artifact struct {
	// ArtifactID is the unique identifier of this artifact.
	ArtifactID string `json:"artifactId"`
	// Name is a human-readable name for the artifact.
	Name string `json:"name,omitempty"`
	// Description is a human-readable explanation of the artifact.
	Description string `json:"description,omitempty"`
	// Parts are the artifact's contents, in order.
	Parts []Part `json:"parts"`
	// Metadata is optional, artifact-level metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// TaskStatus describes the current state of an A2A task.
type TaskStatus struct {
	// State is the current lifecycle state.
	State TaskState `json:"state"`
	// Message is the optional agent-authored status message describing
	// the transition (e.g. why input was required).
	Message *Message `json:"message,omitempty"`
	// Timestamp is an RFC 3339 timestamp of the status update.
	Timestamp string `json:"timestamp,omitempty"`
}

// Task is the central A2A unit of work. A task is created when a client
// issues `message/send`; its state evolves over time until reaching a
// terminal state (completed, canceled, failed, rejected, or unknown).
type Task struct {
	// ID is the unique identifier of this task.
	ID string `json:"id"`
	// ContextID groups multiple tasks that belong to the same
	// conversation.
	ContextID string `json:"contextId,omitempty"`
	// Status is the current state of this task.
	Status TaskStatus `json:"status"`
	// History is the ordered list of messages exchanged so far.
	History []Message `json:"history,omitempty"`
	// Artifacts are the outputs produced by the task so far.
	Artifacts []Artifact `json:"artifacts,omitempty"`
	// Metadata is optional, task-level metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
	// Kind is the on-the-wire discriminator; it is always KindTask
	// when present.
	Kind string `json:"kind,omitempty"`
}

// MessageSendConfiguration tunes how a message/send is processed by the
// agent. Mirrors the optional fields on A2A's `params.configuration`.
type MessageSendConfiguration struct {
	// AcceptedOutputModes are the content/MIME types the client will
	// accept on the response or stream.
	AcceptedOutputModes []string `json:"acceptedOutputModes,omitempty"`
	// HistoryLength caps how many prior messages the agent should
	// include when slicing the conversation. Zero means "no cap".
	HistoryLength int `json:"historyLength,omitempty"`
	// Blocking asks the agent to block until the task reaches a
	// terminal state instead of returning immediately.
	Blocking bool `json:"blocking,omitempty"`
}

// MessageSendParams is the wire-level `params` field of a `message/send`
// JSON-RPC call.
type MessageSendParams struct {
	// Message is the turn being sent.
	Message Message `json:"message"`
	// Configuration optionally tunes how the agent processes the
	// message.
	Configuration *MessageSendConfiguration `json:"configuration,omitempty"`
	// Metadata is optional, call-level metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// TaskQueryParams is the wire-level `params` field of a `tasks/get`
// JSON-RPC call.
type TaskQueryParams struct {
	// ID is the unique identifier of the task being retrieved.
	ID string `json:"id"`
	// HistoryLength caps how many history messages are returned.
	HistoryLength int `json:"historyLength,omitempty"`
	// Metadata is optional, call-level metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// TaskIDParams is the wire-level `params` field for task-id-only JSON-RPC
// calls (e.g. `tasks/cancel`).
type TaskIDParams struct {
	// ID is the unique identifier of the target task.
	ID string `json:"id"`
	// Metadata is optional, call-level metadata.
	Metadata map[string]any `json:"metadata,omitempty"`
}
