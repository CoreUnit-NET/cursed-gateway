package completion_api

import (
	"encoding/json"
	"fmt"
	"strings"

	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

// resolveToolsForMCP applies OpenAI tool_choice as an MCP gate (no prompt text).
// "none" clears tools; a named function filters to that tool; "auto"/omit keeps tools.
func resolveToolsForMCP(tools []cursor_api_sdk.OpenAIToolDef, toolChoice json.RawMessage) ([]cursor_api_sdk.OpenAIToolDef, error) {
	choice := parseToolChoice(toolChoice)
	if choice == "none" {
		return nil, nil
	}
	if name := namedToolChoice(toolChoice); name != "" {
		out := make([]cursor_api_sdk.OpenAIToolDef, 0, 1)
		for _, t := range tools {
			if t.Type != "" && t.Type != "function" {
				continue
			}
			if t.Function.Name == name {
				out = append(out, t)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("tool_choice function %q not found in tools", name)
		}
		return out, nil
	}
	return tools, nil
}

func parseToolChoice(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "auto"
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			return "auto"
		}
		return s
	}
	return "auto"
}

func namedToolChoice(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var obj struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	if obj.Type != "function" {
		return ""
	}
	return strings.TrimSpace(obj.Function.Name)
}
