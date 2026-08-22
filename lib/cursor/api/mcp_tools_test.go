package cursor_api_sdk

import (
	"encoding/json"
	"testing"
)

func TestBuildMcpToolDefinitions(t *testing.T) {
	tools := []OpenAIToolDef{{
		Type: "function",
	}}
	tools[0].Function.Name = "lookup"
	tools[0].Function.Description = "look things up"
	tools[0].Function.Parameters = json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)

	defs, err := BuildMcpToolDefinitions(tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].GetName() != "lookup" || defs[0].GetProviderIdentifier() != mcpProviderIdentifier {
		t.Fatalf("defs = %#v", defs)
	}
	if len(defs[0].GetInputSchema()) == 0 {
		t.Fatal("expected input schema bytes")
	}
}

func TestBuildMcpToolDefinitionsSkipsNonFunctionAndEmptyName(t *testing.T) {
	custom := OpenAIToolDef{Type: "custom"}
	custom.Function.Name = "ignored"
	empty := OpenAIToolDef{Type: "function"}
	keep := OpenAIToolDef{Type: "function"}
	keep.Function.Name = "lookup"
	keep.Function.Parameters = json.RawMessage(`{"type":"object","properties":{}}`)

	defs, err := BuildMcpToolDefinitions([]OpenAIToolDef{custom, empty, keep})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].GetName() != "lookup" {
		t.Fatalf("defs = %#v", defs)
	}
}

func TestDeriveBridgeKeyStable(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "hello world"},
		{Role: "assistant", Content: "hi", ToolCalls: []OpenAIToolCall{{ID: "c1", Type: "function"}}},
		{Role: "tool", Content: "result", ToolCallID: "c1"},
	}
	a := DeriveBridgeKey("auto", msgs)
	b := DeriveBridgeKey("auto", msgs)
	if a == "" || a != b {
		t.Fatalf("keys = %q %q", a, b)
	}
	if DeriveBridgeKey("other", msgs) == a {
		t.Fatal("expected model to affect bridge key")
	}
}

func TestParseChatMessagesToolResults(t *testing.T) {
	parsed := ParseChatMessages([]ChatMessage{
		{Role: "user", Content: "use tools"},
		{Role: "assistant", Content: "", ToolCalls: []OpenAIToolCall{{ID: "c1", Type: "function"}}},
		{Role: "tool", Content: "42", ToolCallID: "c1"},
	})
	if parsed.UserText != "" {
		t.Fatalf("userText = %q", parsed.UserText)
	}
	if len(parsed.ToolResults) != 1 || parsed.ToolResults[0].ToolCallID != "c1" || parsed.ToolResults[0].Content != "42" {
		t.Fatalf("toolResults = %#v", parsed.ToolResults)
	}
}

func TestEncodeMcpRoundTripArgs(t *testing.T) {
	raw, err := encodeJSONSchema(json.RawMessage(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeMcpArgsMap(map[string][]byte{"q": raw})
	if err != nil {
		t.Fatal(err)
	}
	if decoded == "" || decoded == "{}" {
		// struct Value for {"type":"object"} decodes as an object; key q holds it
		t.Fatalf("decoded = %q", decoded)
	}
}
