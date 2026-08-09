package cursor_api_sdk

import "testing"

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

func TestResolveModelID(t *testing.T) {
	if ResolveModelID("") != "default" || ResolveModelID("Auto") != "default" {
		t.Fatal("auto alias failed")
	}
	if ResolveModelID("composer-2.5") != "composer-2.5" {
		t.Fatal("passthrough failed")
	}
}
