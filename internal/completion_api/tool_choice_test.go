package completion_api

import (
	"encoding/json"
	"strings"
	"testing"

	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

func TestResolveToolsForMCP(t *testing.T) {
	lookup := cursor_api_sdk.OpenAIToolDef{Type: "function"}
	lookup.Function.Name = "lookup"
	search := cursor_api_sdk.OpenAIToolDef{Type: "function"}
	search.Function.Name = "search"
	tools := []cursor_api_sdk.OpenAIToolDef{lookup, search}

	out, err := resolveToolsForMCP(tools, nil)
	if err != nil || len(out) != 2 {
		t.Fatalf("omit: out=%#v err=%v", out, err)
	}

	out, err = resolveToolsForMCP(tools, json.RawMessage(`"auto"`))
	if err != nil || len(out) != 2 {
		t.Fatalf("auto: out=%#v err=%v", out, err)
	}

	out, err = resolveToolsForMCP(tools, json.RawMessage(`"none"`))
	if err != nil || out != nil {
		t.Fatalf("none: out=%#v err=%v", out, err)
	}

	out, err = resolveToolsForMCP(tools, json.RawMessage(`{"type":"function","function":{"name":"search"}}`))
	if err != nil || len(out) != 1 || out[0].Function.Name != "search" {
		t.Fatalf("named: out=%#v err=%v", out, err)
	}

	_, err = resolveToolsForMCP(tools, json.RawMessage(`{"type":"function","function":{"name":"missing"}}`))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing named: err=%v", err)
	}

	out, err = resolveToolsForMCP(tools, json.RawMessage(`"required"`))
	if err != nil || len(out) != 2 {
		t.Fatalf("required: out=%#v err=%v", out, err)
	}

	custom := cursor_api_sdk.OpenAIToolDef{Type: "custom"}
	custom.Function.Name = "search"
	out, err = resolveToolsForMCP([]cursor_api_sdk.OpenAIToolDef{custom, search}, json.RawMessage(`{"type":"function","function":{"name":"search"}}`))
	if err != nil || len(out) != 1 || out[0].Type != "function" {
		t.Fatalf("named skips non-function: out=%#v err=%v", out, err)
	}
}
