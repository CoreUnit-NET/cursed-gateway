package cursor_api_sdk

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestParseChatMessagesDeveloperAndFunctionRoles(t *testing.T) {
	parsed := ParseChatMessages([]ChatMessage{
		{Role: "developer", Content: "be brief"},
		{Role: "user", Content: "lookup x"},
		{Role: "assistant", Content: ""},
		{Role: "function", ToolCallID: "call_1", Content: `{"ok":true}`},
	})
	if parsed.SystemPrompt != "be brief" {
		t.Fatalf("system = %q", parsed.SystemPrompt)
	}
	if parsed.UserText != "" {
		t.Fatalf("user = %q, want empty for ResumeAction", parsed.UserText)
	}
	if len(parsed.ToolResults) != 1 || parsed.ToolResults[0].ToolCallID != "call_1" {
		t.Fatalf("ToolResults = %#v", parsed.ToolResults)
	}
	if len(parsed.Turns) != 1 || parsed.Turns[0].UserText != "lookup x" {
		t.Fatalf("turns = %#v", parsed.Turns)
	}
	if len(parsed.Turns[0].ToolResultTexts) != 1 || parsed.Turns[0].ToolResultTexts[0] != `{"ok":true}` {
		t.Fatalf("ToolResultTexts = %#v", parsed.Turns[0].ToolResultTexts)
	}
}

func TestParseChatMessagesEmptyTrailingUserNoFakeReplay(t *testing.T) {
	parsed := ParseChatMessages([]ChatMessage{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	})
	if parsed.UserText != "" {
		t.Fatalf("user = %q, want empty (no fake replay)", parsed.UserText)
	}
	if len(parsed.Turns) != 1 || parsed.Turns[0].UserText != "hi" || parsed.Turns[0].AssistantText != "hello" {
		t.Fatalf("turns = %#v", parsed.Turns)
	}
}

func TestParseChatMessagesToolResultsOnTurn(t *testing.T) {
	parsed := ParseChatMessages([]ChatMessage{
		{Role: "user", Content: "lookup x"},
		{Role: "assistant", Content: ""},
		{Role: "tool", ToolCallID: "call_1", Content: `{"ok":true}`},
	})
	if parsed.UserText != "" {
		t.Fatalf("user = %q, want empty for ResumeAction", parsed.UserText)
	}
	if len(parsed.ToolResults) != 1 || parsed.ToolResults[0].ToolCallID != "call_1" {
		t.Fatalf("ToolResults = %#v", parsed.ToolResults)
	}
	if len(parsed.Turns) != 1 {
		t.Fatalf("turns = %#v", parsed.Turns)
	}
	tr := parsed.Turns[0]
	if tr.UserText != "lookup x" {
		t.Fatalf("turn user = %q", tr.UserText)
	}
	if len(tr.ToolResultTexts) != 1 || tr.ToolResultTexts[0] != `{"ok":true}` {
		t.Fatalf("ToolResultTexts = %#v", tr.ToolResultTexts)
	}
}

func TestCapToolResult(t *testing.T) {
	short := "hello"
	if got := CapToolResult(short); got != short {
		t.Fatalf("short=%q", got)
	}

	exact := strings.Repeat("a", MaxToolResultChars)
	if got := CapToolResult(exact); got != exact {
		t.Fatalf("exact len=%d", len(got))
	}

	over := strings.Repeat("b", MaxToolResultChars+50)
	got := CapToolResult(over)
	if !strings.HasSuffix(got, "\n...[truncated]") {
		t.Fatalf("missing truncation marker: %q", got[len(got)-20:])
	}
	body := strings.TrimSuffix(got, "\n...[truncated]")
	if len(body) > MaxToolResultChars {
		t.Fatalf("body len=%d want <= %d", len(body), MaxToolResultChars)
	}
	if !utf8.ValidString(got) {
		t.Fatal("truncated output must be valid UTF-8")
	}

	// Multi-byte rune straddling the cut must not leave invalid UTF-8.
	prefix := strings.Repeat("x", MaxToolResultChars-1)
	multi := prefix + "⌘more"
	got = CapToolResult(multi)
	if !utf8.ValidString(got) {
		t.Fatal("utf8-safe truncate failed")
	}
	if !strings.HasSuffix(got, "\n...[truncated]") {
		t.Fatalf("expected truncation: %q", got)
	}
}

func TestParseChatMessagesCapsToolResults(t *testing.T) {
	huge := strings.Repeat("z", MaxToolResultChars+100)
	parsed := ParseChatMessages([]ChatMessage{
		{Role: "user", Content: "lookup x"},
		{Role: "assistant", Content: ""},
		{Role: "tool", ToolCallID: "call_1", Content: huge},
	})
	if len(parsed.ToolResults) != 1 {
		t.Fatalf("ToolResults=%#v", parsed.ToolResults)
	}
	if parsed.ToolResults[0].Content == huge {
		t.Fatal("expected CapToolResult on ToolResults")
	}
	if !strings.HasSuffix(parsed.ToolResults[0].Content, "\n...[truncated]") {
		t.Fatalf("ToolResults content=%q", parsed.ToolResults[0].Content[len(parsed.ToolResults[0].Content)-20:])
	}
	if len(parsed.Turns) != 1 || len(parsed.Turns[0].ToolResultTexts) != 1 {
		t.Fatalf("turns=%#v", parsed.Turns)
	}
	if parsed.Turns[0].ToolResultTexts[0] != parsed.ToolResults[0].Content {
		t.Fatalf("ToolResultTexts mismatch: %#v vs %#v", parsed.Turns[0].ToolResultTexts[0], parsed.ToolResults[0].Content)
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

func TestBuildRunPayloadBlobifiesMultiTurn(t *testing.T) {
	payload, err := BuildRunPayload("composer-2.5", ParsedChat{
		SystemPrompt: "sys",
		Turns: []ConversationTurn{
			{UserText: "hi", AssistantText: "hello"},
			{UserText: "again", AssistantText: "ok"},
		},
		UserText: "bye",
	})
	if err != nil {
		t.Fatal(err)
	}

	var msg cursorProto.AgentClientMessage
	if err := proto.Unmarshal(payload.RequestBytes, &msg); err != nil {
		t.Fatal(err)
	}
	run := msg.GetRunRequest()
	if run == nil || run.ConversationState == nil {
		t.Fatal("missing run / conversation state")
	}
	state := run.ConversationState

	// system + 2 user + 2 assistant root role blobs
	if got := len(state.RootPromptMessagesJson); got != 5 {
		t.Fatalf("rootPromptMessagesJson len = %d, want 5", got)
	}
	if got := len(state.Turns); got != 2 {
		t.Fatalf("turns len = %d, want 2", got)
	}

	assertBlobID := func(t *testing.T, label string, id []byte) {
		t.Helper()
		if len(id) != 32 {
			t.Fatalf("%s: id len = %d, want 32", label, len(id))
		}
		key := hex.EncodeToString(id)
		raw, ok := payload.BlobStore[key]
		if !ok {
			t.Fatalf("%s: hex id %s not in BlobStore", label, key)
		}
		if len(raw) == 0 {
			t.Fatalf("%s: empty blob data", label)
		}
		// Structure slots must be ids, not inlined protobuf payloads.
		if len(raw) == 32 && hex.EncodeToString(raw) == key {
			t.Fatalf("%s: blob data looks like another id (unexpected)", label)
		}
	}

	for i, id := range state.RootPromptMessagesJson {
		assertBlobID(t, fmt.Sprintf("root[%d]", i), id)
	}
	for i, turnID := range state.Turns {
		assertBlobID(t, fmt.Sprintf("turn[%d]", i), turnID)
		turnRaw := payload.BlobStore[hex.EncodeToString(turnID)]
		var turnStruct cursorProto.ConversationTurnStructure
		if err := proto.Unmarshal(turnRaw, &turnStruct); err != nil {
			t.Fatalf("turn[%d] unmarshal: %v", i, err)
		}
		agent := turnStruct.GetAgentConversationTurn()
		if agent == nil {
			t.Fatalf("turn[%d]: missing agent turn", i)
		}
		assertBlobID(t, fmt.Sprintf("turn[%d].userMessage", i), agent.UserMessage)
		if len(agent.Steps) != 1 {
			t.Fatalf("turn[%d]: steps = %d, want 1", i, len(agent.Steps))
		}
		assertBlobID(t, fmt.Sprintf("turn[%d].step[0]", i), agent.Steps[0])
	}

	// Live user message stays on the action (not only history).
	uma := run.GetAction().GetUserMessageAction()
	if uma == nil || uma.UserMessage == nil || uma.UserMessage.Text != "bye" {
		t.Fatalf("user message action = %#v", uma)
	}

	// Expect: 5 root JSON + 2 userMsg + 2 steps + 2 turn envelopes = 11
	if got := len(payload.BlobStore); got != 11 {
		t.Fatalf("BlobStore size = %d, want 11", got)
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

func TestParseChatMessagesKeepsActionImages(t *testing.T) {
	png, err := json.Marshal([]map[string]any{
		{"type": "text", "text": "describe"},
		{"type": "image_url", "image_url": map[string]string{
			"url": "data:image/png;base64," + tinyPNGBase64,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var action ChatMessage
	if err := json.Unmarshal([]byte(`{"role":"user","content":`+string(png)+`}`), &action); err != nil {
		t.Fatal(err)
	}
	parsed := ParseChatMessages([]ChatMessage{
		{Role: "user", Content: "earlier", Images: []Image{{Bytes: []byte("old"), MimeType: "image/png", Filename: "old.png"}}},
		{Role: "assistant", Content: "ok"},
		action,
	})
	if parsed.UserText != "describe" {
		t.Fatalf("user = %q", parsed.UserText)
	}
	if len(parsed.UserImages) != 1 {
		t.Fatalf("UserImages = %d, want 1", len(parsed.UserImages))
	}
	if len(parsed.Turns) != 1 || parsed.Turns[0].UserText != "earlier" {
		t.Fatalf("turns = %#v", parsed.Turns)
	}
}

func TestParseChatMessagesImageOnlyAction(t *testing.T) {
	raw := `{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,` + tinyPNGBase64 + `"}}]}`
	var msg ChatMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatal(err)
	}
	parsed := ParseChatMessages([]ChatMessage{msg})
	if parsed.UserText != "" {
		t.Fatalf("user text = %q, want empty", parsed.UserText)
	}
	if len(parsed.UserImages) != 1 {
		t.Fatalf("UserImages = %d", len(parsed.UserImages))
	}
}

func TestBuildRunPayloadAttachesSelectedImages(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := BuildRunPayload("composer-2.5", ParsedChat{
		SystemPrompt: "sys",
		UserText:     "what is this?",
		UserImages: []Image{{
			Bytes:    png,
			MimeType: "image/png",
			Filename: "dot.png",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var msg cursorProto.AgentClientMessage
	if err := proto.Unmarshal(payload.RequestBytes, &msg); err != nil {
		t.Fatal(err)
	}
	uma := msg.GetRunRequest().GetAction().GetUserMessageAction()
	if uma == nil || uma.UserMessage == nil {
		t.Fatal("missing user message action")
	}
	sc := uma.UserMessage.GetSelectedContext()
	if sc == nil || len(sc.SelectedImages) != 1 {
		t.Fatalf("selected images = %#v", sc)
	}
	img := sc.SelectedImages[0]
	if img.Path != "dot.png" || img.MimeType != "image/png" {
		t.Fatalf("image meta = %#v", img)
	}
	bd := img.GetBlobIdWithData()
	if bd == nil || len(bd.BlobId) != 32 || string(bd.Data) != string(png) {
		t.Fatalf("blobIdWithData = %#v", bd)
	}
	key := hex.EncodeToString(bd.BlobId)
	if got := payload.BlobStore[key]; string(got) != string(png) {
		t.Fatalf("blob store missing image bytes for %s", key)
	}
}

func TestBuildRunPayloadImageOnlyOK(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(tinyPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := BuildRunPayload("composer-2.5", ParsedChat{
		SystemPrompt: "sys",
		UserImages:   []Image{{Bytes: png, MimeType: "image/png", Filename: "dot.png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var msg cursorProto.AgentClientMessage
	if err := proto.Unmarshal(payload.RequestBytes, &msg); err != nil {
		t.Fatal(err)
	}
	uma := msg.GetRunRequest().GetAction().GetUserMessageAction()
	if uma.UserMessage.Text != "" {
		t.Fatalf("text = %q", uma.UserMessage.Text)
	}
	if len(uma.UserMessage.GetSelectedContext().GetSelectedImages()) != 1 {
		t.Fatal("expected one selected image")
	}
}

func TestBuildRunPayloadMissingUserStillErrors(t *testing.T) {
	_, err := BuildRunPayload("composer-2.5", ParsedChat{SystemPrompt: "sys"})
	if err == nil {
		t.Fatal("expected missing user error")
	}
	if !strings.Contains(err.Error(), "missing user message") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildRunPayloadResumeAction(t *testing.T) {
	payload, err := BuildRunPayload("composer-2.5", ParsedChat{
		SystemPrompt: "sys",
		Turns: []ConversationTurn{
			{UserText: "hi", AssistantText: "hello"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var msg cursorProto.AgentClientMessage
	if err := proto.Unmarshal(payload.RequestBytes, &msg); err != nil {
		t.Fatal(err)
	}
	run := msg.GetRunRequest()
	if run == nil || run.GetAction() == nil {
		t.Fatal("missing run action")
	}
	if run.GetAction().GetResumeAction() == nil {
		t.Fatalf("expected ResumeAction, got %#v", run.GetAction())
	}
	if run.GetAction().GetUserMessageAction() != nil {
		t.Fatal("must not emit UserMessageAction for empty trailing user")
	}
	if got := len(run.ConversationState.Turns); got != 1 {
		t.Fatalf("turns = %d, want 1", got)
	}
}

func TestBuildRunPayloadResumeIncludesToolResults(t *testing.T) {
	payload, err := BuildRunPayload("composer-2.5", ParsedChat{
		SystemPrompt: "sys",
		Turns: []ConversationTurn{{
			UserText:        "lookup x",
			AssistantText:   "",
			ToolResultTexts: []string{`{"ok":true}`},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var msg cursorProto.AgentClientMessage
	if err := proto.Unmarshal(payload.RequestBytes, &msg); err != nil {
		t.Fatal(err)
	}
	run := msg.GetRunRequest()
	if run.GetAction().GetResumeAction() == nil {
		t.Fatalf("expected ResumeAction, got %#v", run.GetAction())
	}
	// system + user + tool-result root blobs
	if got := len(run.ConversationState.RootPromptMessagesJson); got != 3 {
		t.Fatalf("root len = %d, want 3", got)
	}
	foundTool := false
	for _, id := range run.ConversationState.RootPromptMessagesJson {
		raw := payload.BlobStore[hex.EncodeToString(id)]
		var root rootRoleMessage
		if err := json.Unmarshal(raw, &root); err != nil {
			t.Fatal(err)
		}
		if root.Role != "user" {
			continue
		}
		parts, _ := root.Content.([]any)
		if len(parts) == 0 {
			// Content may decode as []rootTextPart via typed JSON — check string form.
			s := string(raw)
			if strings.Contains(s, "[Tool Result]") && strings.Contains(s, `{"ok":true}`) {
				foundTool = true
				break
			}
			continue
		}
		if strings.Contains(string(raw), "[Tool Result]") {
			foundTool = true
			break
		}
	}
	if !foundTool {
		t.Fatal("expected [Tool Result] root blob")
	}
	if got := len(run.ConversationState.Turns); got != 1 {
		t.Fatalf("turns = %d", got)
	}
	turnRaw := payload.BlobStore[hex.EncodeToString(run.ConversationState.Turns[0])]
	var turnStruct cursorProto.ConversationTurnStructure
	if err := proto.Unmarshal(turnRaw, &turnStruct); err != nil {
		t.Fatal(err)
	}
	agent := turnStruct.GetAgentConversationTurn()
	if agent == nil || len(agent.Steps) != 1 {
		t.Fatalf("agent steps = %#v", agent)
	}
	stepRaw := payload.BlobStore[hex.EncodeToString(agent.Steps[0])]
	var step cursorProto.ConversationStep
	if err := proto.Unmarshal(stepRaw, &step); err != nil {
		t.Fatal(err)
	}
	am := step.GetAssistantMessage()
	if am == nil || !strings.HasPrefix(am.Text, "[Tool Result]\n") {
		t.Fatalf("step = %#v", am)
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
