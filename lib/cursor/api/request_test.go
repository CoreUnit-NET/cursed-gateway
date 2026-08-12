package cursor_api_sdk

import (
	"encoding/json"
	"testing"

	cursorProto "github.com/CoreUnit-NET/cursed-gateway/lib/cursorProto"
	"google.golang.org/protobuf/proto"
)

func TestParseChatMessages(t *testing.T) {
	parsed := ParseChatMessages([]ChatMessage{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
		{Role: "user", Content: "bye"},
	})
	if parsed.SystemPrompt != "be brief" {
		t.Fatalf("system = %q", parsed.SystemPrompt)
	}
	if parsed.UserText != "bye" {
		t.Fatalf("user = %q", parsed.UserText)
	}
	if len(parsed.Turns) != 1 || parsed.Turns[0].UserText != "hi" || parsed.Turns[0].AssistantText != "hello" {
		t.Fatalf("turns = %#v", parsed.Turns)
	}
}

func TestBuildRunPayload(t *testing.T) {
	payload, err := BuildRunPayload("auto", ParsedChat{
		SystemPrompt: "sys",
		UserText:     "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if payload.ModelID != "default" {
		t.Fatalf("model = %q", payload.ModelID)
	}
	if len(payload.RequestBytes) == 0 {
		t.Fatal("empty request bytes")
	}
	if len(payload.BlobStore) != 1 {
		t.Fatalf("blob store size = %d", len(payload.BlobStore))
	}
}

func TestBuildRunPayloadSelectionUsesWireModelID(t *testing.T) {
	payload, err := BuildRunPayloadSelection(ModelSelection{
		PublicID:    "claude-haiku-4-5",
		WireModelID: "claude-4.5-haiku-thinking",
		DisplayName: "Haiku 4.5",
		Parameters:  []ModelParameter{{ID: "thinking", Value: "true"}},
	}, ParsedChat{SystemPrompt: "sys", UserText: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if payload.ModelID != "claude-haiku-4-5" {
		t.Fatalf("response model id = %q", payload.ModelID)
	}
	var msg cursorProto.AgentClientMessage
	if err := proto.Unmarshal(payload.RequestBytes, &msg); err != nil {
		t.Fatal(err)
	}
	run := msg.GetRunRequest()
	if run == nil || run.ModelDetails == nil || run.RequestedModel == nil {
		t.Fatalf("missing run fields: %#v", run)
	}
	if got := run.ModelDetails.GetModelId(); got != "claude-4.5-haiku-thinking" {
		t.Fatalf("ModelDetails.model_id = %q", got)
	}
	if got := run.ModelDetails.GetDisplayModelId(); got != "claude-haiku-4-5" {
		t.Fatalf("ModelDetails.display_model_id = %q", got)
	}
	if got := run.RequestedModel.GetModelId(); got != "claude-4.5-haiku-thinking" {
		t.Fatalf("RequestedModel.model_id = %q", got)
	}
}

func TestResolveModelID(t *testing.T) {
	if ResolveModelID("") != "default" || ResolveModelID("Auto") != "default" {
		t.Fatal("auto alias failed")
	}
	if ResolveModelID("composer-2.5") != "composer-2.5" {
		t.Fatal("passthrough failed")
	}
}

func TestMapAvailableModelLegacySlug(t *testing.T) {
	raw := json.RawMessage(`{
		"name": "claude-haiku-4-5",
		"supportsAgent": true,
		"supportsThinking": true,
		"supportsMaxMode": true,
		"supportsNonMaxMode": true,
		"clientDisplayName": "Haiku 4.5",
		"serverModelName": "claude-haiku-4-5",
		"idAliases": ["haiku", "haiku-4.5"],
		"legacySlugs": ["claude-4.5-haiku", "claude-4.5-haiku-thinking"],
		"variants": [
			{
				"parameterValues": [{"id": "thinking", "value": "false"}],
				"legacySlug": "claude-4.5-haiku"
			},
			{
				"parameterValues": [{"id": "thinking", "value": "true"}],
				"isDefaultMaxConfig": true,
				"isDefaultNonMaxConfig": true,
				"legacySlug": "claude-4.5-haiku-thinking"
			}
		]
	}`)
	m, ok := mapAvailableModel(raw)
	if !ok {
		t.Fatal("map failed")
	}
	if m.ID != "claude-haiku-4-5" {
		t.Fatalf("id = %q", m.ID)
	}
	if m.LegacySlug != "claude-4.5-haiku-thinking" {
		t.Fatalf("legacy = %q", m.LegacySlug)
	}
	if m.MaxMode {
		t.Fatal("expected max_mode=false for default non-max variant")
	}
	if len(m.Parameters) != 1 || m.Parameters[0].ID != "thinking" || m.Parameters[0].Value != "true" {
		t.Fatalf("params = %#v", m.Parameters)
	}
	sel := SelectionFromModel(m)
	if sel.PublicID != "claude-haiku-4-5" || sel.WireModelID != "claude-4.5-haiku-thinking" {
		t.Fatalf("selection = %#v", sel)
	}
	if !modelMatchesID(m, "haiku") || !modelMatchesID(m, "claude-4.5-haiku") {
		t.Fatal("alias/legacy match failed")
	}
}
