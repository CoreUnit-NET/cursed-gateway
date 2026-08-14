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

func TestChatMessageUnmarshalContent(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "string", raw: `{"role":"user","content":"hello"}`, want: "hello"},
		{name: "null", raw: `{"role":"assistant","content":null}`},
		{name: "omitted", raw: `{"role":"assistant"}`},
		{name: "text parts", raw: `{"role":"user","content":[{"type":"text","text":"hello"},{"type":"text","text":"world"}]}`, want: "hello\nworld"},
		{name: "string parts", raw: `{"role":"user","content":["hello","world"]}`, want: "hello\nworld"},
		{name: "mixed image skipped", raw: `{"role":"user","content":[{"type":"text","text":"caption"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}`, want: "caption"},
		{name: "input_text", raw: `{"role":"user","content":[{"type":"input_text","text":"hi"}]}`, want: "hi"},
		{name: "single part object", raw: `{"role":"user","content":{"type":"text","text":"solo"}}`, want: "solo"},
		{name: "empty array", raw: `{"role":"user","content":[]}`},
		{name: "number rejected", raw: `{"role":"user","content":1}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var msg ChatMessage
			err := json.Unmarshal([]byte(tc.raw), &msg)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if msg.Content != tc.want {
				t.Fatalf("content = %q, want %q", msg.Content, tc.want)
			}
		})
	}
}

func TestChatCompletionRequestArrayContent(t *testing.T) {
	// OpenCode / OpenAI multipart body that previously failed:
	// cannot unmarshal array into ... ChatMessage ... content of type string
	raw := []byte(`{
		"model": "composer-2.5",
		"stream": true,
		"messages": [
			{"role":"system","content":[{"type":"text","text":"be brief"}]},
			{"role":"user","content":[{"type":"text","text":"hello"}]}
		]
	}`)
	var req struct {
		Model    string        `json:"model"`
		Messages []ChatMessage `json:"messages"`
		Stream   bool          `json:"stream"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	parsed := ParseChatMessages(req.Messages)
	if parsed.SystemPrompt != "be brief" {
		t.Fatalf("system = %q", parsed.SystemPrompt)
	}
	if parsed.UserText != "hello" {
		t.Fatalf("user = %q", parsed.UserText)
	}
}

func TestChatMessageUnmarshalToolCalls(t *testing.T) {
	raw := []byte(`{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"fn","arguments":"{}"}}]}`)
	var msg ChatMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Role != "assistant" || msg.Content != "" {
		t.Fatalf("msg = %#v", msg)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "c1" || msg.ToolCalls[0].Function.Name != "fn" {
		t.Fatalf("tool_calls = %#v", msg.ToolCalls)
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
