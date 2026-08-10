package cursor_api_sdk

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	cursorProto "github.com/CoreUnit-NET/cursed-gateway/lib/cursorProto"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

const mcpProviderIdentifier = "opencode"

// OpenAIToolDef is the OpenAI tools[] entry we accept.
type OpenAIToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

// OpenAIToolCall is an assistant tool_calls[] entry.
type OpenAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// PendingExec is a Cursor mcpArgs call waiting for an OpenAI tool result.
type PendingExec struct {
	ExecID      string
	ExecMsgID   uint32
	ToolCallID  string
	ToolName    string
	DecodedArgs string
}

// ToolResultInfo is a role=tool message payload.
type ToolResultInfo struct {
	ToolCallID string
	Content    string
}

// BuildMcpToolDefinitions maps OpenAI tool defs to Cursor MCP descriptors.
func BuildMcpToolDefinitions(tools []OpenAIToolDef) ([]*cursorProto.McpToolDefinition, error) {
	out := make([]*cursorProto.McpToolDefinition, 0, len(tools))
	for _, t := range tools {
		name := t.Function.Name
		if name == "" {
			continue
		}
		schemaBytes, err := encodeJSONSchema(t.Function.Parameters)
		if err != nil {
			return nil, fmt.Errorf("tool %q parameters: %w", name, err)
		}
		out = append(out, &cursorProto.McpToolDefinition{
			Name:               name,
			Description:        t.Function.Description,
			ProviderIdentifier: mcpProviderIdentifier,
			ToolName:           name,
			InputSchema:        schemaBytes,
		})
	}
	return out, nil
}

func encodeJSONSchema(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	v := &structpb.Value{}
	if err := protojson.Unmarshal(raw, v); err != nil {
		return nil, err
	}
	return proto.Marshal(v)
}

// DecodeMcpArgsMap decodes Cursor MCP arg Value bytes into JSON object text.
func DecodeMcpArgsMap(args map[string][]byte) (string, error) {
	decoded := map[string]any{}
	for k, raw := range args {
		decoded[k] = decodeMcpArgValue(raw)
	}
	b, err := json.Marshal(decoded)
	if err != nil {
		return "{}", err
	}
	return string(b), nil
}

func decodeMcpArgValue(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	v := &structpb.Value{}
	if err := proto.Unmarshal(raw, v); err == nil {
		return v.AsInterface()
	}
	return string(raw)
}

// EncodeMcpSuccess builds a text mcpResult success payload.
func EncodeMcpSuccess(text string) *cursorProto.McpResult {
	return &cursorProto.McpResult{
		Result: &cursorProto.McpResult_Success{
			Success: &cursorProto.McpSuccess{
				Content: []*cursorProto.McpToolResultContentItem{{
					Content: &cursorProto.McpToolResultContentItem_Text{
						Text: &cursorProto.McpTextContent{Text: text},
					},
				}},
				IsError: false,
			},
		},
	}
}

// EncodeMcpError builds an mcpResult error payload.
func EncodeMcpError(msg string) *cursorProto.McpResult {
	return &cursorProto.McpResult{
		Result: &cursorProto.McpResult_Error{
			Error: &cursorProto.McpError{Error: msg},
		},
	}
}

// DeriveBridgeKey builds a stable key for parking/resuming a Run across tool turns.
func DeriveBridgeKey(modelID string, messages []ChatMessage) string {
	firstUser := ""
	for _, m := range messages {
		if m.Role == "user" {
			firstUser = m.Content
			break
		}
	}
	if len(firstUser) > 200 {
		firstUser = firstUser[:200]
	}
	sum := sha256.Sum256([]byte(modelID + ":" + firstUser))
	return hex.EncodeToString(sum[:])[:16]
}
