package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/hurtener/Portico_gateway/internal/a2a/protocol"
)

func TestA2AProtocol_TaskState_Values(t *testing.T) {
	// Wire values are fixed by the A2A spec. A typo here (e.g.
	// "cancelled" vs "canceled", or "input_required" vs
	// "input-required") would silently break every interop with an
	// external A2A client — keep this test as the canonical guard.
	cases := []struct {
		name string
		got  protocol.TaskState
		want string
	}{
		{"submitted", protocol.TaskStateSubmitted, "submitted"},
		{"working", protocol.TaskStateWorking, "working"},
		{"input-required", protocol.TaskStateInputRequired, "input-required"},
		{"completed", protocol.TaskStateCompleted, "completed"},
		{"canceled", protocol.TaskStateCanceled, "canceled"},
		{"failed", protocol.TaskStateFailed, "failed"},
		{"rejected", protocol.TaskStateRejected, "rejected"},
		{"auth-required", protocol.TaskStateAuthRequired, "auth-required"},
		{"unknown", protocol.TaskStateUnknown, "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if string(c.got) != c.want {
				t.Errorf("TaskState %q on wire, want %q", string(c.got), c.want)
			}
		})
	}
}

func TestA2AProtocol_Part_Kinds(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		in := protocol.Part{Kind: protocol.PartKindText, Text: "hello world"}
		body, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var out protocol.Part
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if out.Kind != protocol.PartKindText {
			t.Errorf("Kind = %q, want %q", out.Kind, protocol.PartKindText)
		}
		if out.Text != "hello world" {
			t.Errorf("Text = %q, want %q", out.Text, "hello world")
		}
		if out.File != nil {
			t.Errorf("File should be nil (and not survive round trip), got %+v", out.File)
		}
		if out.Data != nil {
			t.Errorf("Data should be nil on a text part, got %+v", out.Data)
		}
	})

	t.Run("file", func(t *testing.T) {
		in := protocol.Part{
			Kind: protocol.PartKindFile,
			File: &protocol.FileContent{
				Name:     "report.pdf",
				MimeType: "application/pdf",
				Bytes:    "VGhpcyBpcyBhIHNob3J0IHRleHQu",
			},
		}
		body, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var out protocol.Part
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if out.Kind != protocol.PartKindFile {
			t.Errorf("Kind = %q, want %q", out.Kind, protocol.PartKindFile)
		}
		if out.File == nil {
			t.Fatalf("File should survive round trip")
		}
		if out.File.Name != "report.pdf" || out.File.MimeType != "application/pdf" {
			t.Errorf("File metadata lost: %+v", out.File)
		}
		if out.File.Bytes != "VGhpcyBpcyBhIHNob3J0IHRleHQu" {
			t.Errorf("File.Bytes = %q", out.File.Bytes)
		}
		if out.File.URI != "" {
			t.Errorf("File.URI should be empty when Bytes is set, got %q", out.File.URI)
		}
		if out.Text != "" {
			t.Errorf("Text should be empty on a file part, got %q", out.Text)
		}
		if out.Data != nil {
			t.Errorf("Data should be nil on a file part, got %+v", out.Data)
		}
	})

	t.Run("data", func(t *testing.T) {
		in := protocol.Part{
			Kind: protocol.PartKindData,
			Data: map[string]any{
				"score":   float64(0.91),
				"label":   "spam",
				"details": []any{"a", "b"},
			},
		}
		body, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var out protocol.Part
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if out.Kind != protocol.PartKindData {
			t.Errorf("Kind = %q, want %q", out.Kind, protocol.PartKindData)
		}
		if out.Data == nil {
			t.Fatalf("Data should survive round trip")
		}
		if got := out.Data["label"]; got != "spam" {
			t.Errorf("Data[label] = %v, want spam", got)
		}
		if got := out.Data["score"]; got != float64(0.91) {
			t.Errorf("Data[score] = %v, want 0.91", got)
		}
		if out.Text != "" {
			t.Errorf("Text should be empty on a data part, got %q", out.Text)
		}
		if out.File != nil {
			t.Errorf("File should be nil on a data part, got %+v", out.File)
		}
	})
}

func TestA2AProtocol_MessageSendParams_RoundTrip(t *testing.T) {
	in := protocol.MessageSendParams{
		Message: protocol.Message{
			Role:      protocol.RoleUser,
			MessageID: "msg-001",
			ContextID: "ctx-001",
			Kind:      protocol.KindMessage,
			Parts: []protocol.Part{
				{Kind: protocol.PartKindText, Text: "Summarise the diff."},
			},
		},
		Configuration: &protocol.MessageSendConfiguration{
			AcceptedOutputModes: []string{"application/json", "text/plain"},
			HistoryLength:       5,
			Blocking:            true,
		},
		Metadata: map[string]any{
			"traceId": "abc-123",
		},
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out protocol.MessageSendParams
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Message.Role != protocol.RoleUser {
		t.Errorf("Message.Role = %q, want %q", out.Message.Role, protocol.RoleUser)
	}
	if out.Message.MessageID != "msg-001" {
		t.Errorf("Message.MessageID = %q", out.Message.MessageID)
	}
	if out.Message.ContextID != "ctx-001" {
		t.Errorf("Message.ContextID = %q", out.Message.ContextID)
	}
	if out.Message.Kind != protocol.KindMessage {
		t.Errorf("Message.Kind = %q, want %q", out.Message.Kind, protocol.KindMessage)
	}
	if len(out.Message.Parts) != 1 || out.Message.Parts[0].Kind != protocol.PartKindText {
		t.Fatalf("Message.Parts lost: %+v", out.Message.Parts)
	}
	if out.Message.Parts[0].Text != "Summarise the diff." {
		t.Errorf("Message.Parts[0].Text = %q", out.Message.Parts[0].Text)
	}
	if out.Configuration == nil {
		t.Fatalf("Configuration lost in round trip")
	}
	if len(out.Configuration.AcceptedOutputModes) != 2 ||
		out.Configuration.AcceptedOutputModes[1] != "text/plain" {
		t.Errorf("AcceptedOutputModes lost: %+v", out.Configuration.AcceptedOutputModes)
	}
	if out.Configuration.HistoryLength != 5 {
		t.Errorf("HistoryLength = %d, want 5", out.Configuration.HistoryLength)
	}
	if !out.Configuration.Blocking {
		t.Errorf("Blocking = false, want true")
	}
	if out.Metadata == nil || out.Metadata["traceId"] != "abc-123" {
		t.Errorf("Metadata lost: %+v", out.Metadata)
	}
}

func TestA2AProtocol_Task_RoundTrip(t *testing.T) {
	in := protocol.Task{
		ID:        "task-42",
		ContextID: "ctx-001",
		Kind:      protocol.KindTask,
		Status: protocol.TaskStatus{
			State:     protocol.TaskStateWorking,
			Timestamp: "2026-06-19T12:34:56Z",
			Message: &protocol.Message{
				Role:      protocol.RoleAgent,
				MessageID: "msg-status-1",
				Kind:      protocol.KindMessage,
				Parts: []protocol.Part{
					{Kind: protocol.PartKindText, Text: "Working on it..."},
				},
			},
		},
		History: []protocol.Message{
			{
				Role:      protocol.RoleUser,
				MessageID: "msg-user-1",
				Kind:      protocol.KindMessage,
				Parts: []protocol.Part{
					{Kind: protocol.PartKindText, Text: "Please summarise."},
				},
			},
		},
		Artifacts: []protocol.Artifact{
			{
				ArtifactID:  "artifact-1",
				Name:        "summary",
				Description: "A short summary of the diff",
				Parts: []protocol.Part{
					{
						Kind: protocol.PartKindData,
						Data: map[string]any{
							"title": "TL;DR",
							"lines": float64(3),
						},
					},
				},
			},
		},
		Metadata: map[string]any{
			"tenantId": "t-1",
		},
	}
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out protocol.Task
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != "task-42" {
		t.Errorf("ID = %q, want %q", out.ID, "task-42")
	}
	if out.ContextID != "ctx-001" {
		t.Errorf("ContextID = %q", out.ContextID)
	}
	if out.Kind != protocol.KindTask {
		t.Errorf("Kind = %q, want %q", out.Kind, protocol.KindTask)
	}
	if out.Status.State != protocol.TaskStateWorking {
		t.Errorf("Status.State = %q, want %q", out.Status.State, protocol.TaskStateWorking)
	}
	if out.Status.Timestamp != "2026-06-19T12:34:56Z" {
		t.Errorf("Status.Timestamp = %q", out.Status.Timestamp)
	}
	if out.Status.Message == nil {
		t.Fatalf("Status.Message should survive round trip")
	}
	if out.Status.Message.Role != protocol.RoleAgent {
		t.Errorf("Status.Message.Role = %q", out.Status.Message.Role)
	}
	if len(out.Status.Message.Parts) != 1 ||
		out.Status.Message.Parts[0].Kind != protocol.PartKindText ||
		out.Status.Message.Parts[0].Text != "Working on it..." {
		t.Errorf("Status.Message.Parts lost: %+v", out.Status.Message.Parts)
	}
	if out.Metadata == nil || out.Metadata["tenantId"] != "t-1" {
		t.Errorf("Metadata lost: %+v", out.Metadata)
	}
	if len(out.History) != 1 {
		t.Fatalf("History length = %d, want 1", len(out.History))
	}
	if out.History[0].Role != protocol.RoleUser {
		t.Errorf("History[0].Role = %q", out.History[0].Role)
	}
	if len(out.Artifacts) != 1 {
		t.Fatalf("Artifacts length = %d, want 1", len(out.Artifacts))
	}
	if out.Artifacts[0].ArtifactID != "artifact-1" {
		t.Errorf("Artifacts[0].ArtifactID = %q", out.Artifacts[0].ArtifactID)
	}
	if len(out.Artifacts[0].Parts) != 1 ||
		out.Artifacts[0].Parts[0].Kind != protocol.PartKindData {
		t.Fatalf("Artifacts[0].Parts[0] lost: %+v", out.Artifacts[0].Parts)
	}
	if out.Artifacts[0].Parts[0].Data == nil ||
		out.Artifacts[0].Parts[0].Data["title"] != "TL;DR" {
		t.Errorf("Artifacts[0].Parts[0].Data lost: %+v", out.Artifacts[0].Parts[0].Data)
	}
}
