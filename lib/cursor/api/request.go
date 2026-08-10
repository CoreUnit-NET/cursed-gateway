package cursor_api_sdk

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	cursorProto "github.com/CoreUnit-NET/cursed-gateway/lib/cursorProto"
	"google.golang.org/protobuf/proto"
)

// ChatMessage is a minimal OpenAI chat message (tools-aware).
type ChatMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
}

// ConversationTurn is a prior user/assistant pair.
type ConversationTurn struct {
	UserText      string
	AssistantText string
}

// ParsedChat is the OpenAI → Cursor mapping of a chat request.
type ParsedChat struct {
	SystemPrompt string
	Turns        []ConversationTurn
	UserText     string
	ToolResults  []ToolResultInfo
}

// ParseChatMessages splits OpenAI messages into system / history / current user / tool results.
func ParseChatMessages(messages []ChatMessage) ParsedChat {
	var systems []string
	var turns []ConversationTurn
	var pendingUser string
	var toolResults []ToolResultInfo

	for _, m := range messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		text := strings.TrimSpace(m.Content)
		switch role {
		case "system":
			if text != "" {
				systems = append(systems, text)
			}
		case "tool":
			toolResults = append(toolResults, ToolResultInfo{
				ToolCallID: strings.TrimSpace(m.ToolCallID),
				Content:    m.Content,
			})
		case "user":
			if pendingUser != "" {
				turns = append(turns, ConversationTurn{UserText: pendingUser})
			}
			pendingUser = text
		case "assistant":
			// Skip tool_calls-only assistants with no text (continuation is via mcpResult).
			if pendingUser != "" {
				turns = append(turns, ConversationTurn{UserText: pendingUser, AssistantText: text})
				pendingUser = ""
			}
		}
	}

	system := strings.Join(systems, "\n")
	if system == "" {
		system = "You are a helpful assistant."
	}

	userText := pendingUser
	if userText == "" && len(toolResults) == 0 && len(turns) > 0 {
		last := turns[len(turns)-1]
		turns = turns[:len(turns)-1]
		userText = last.UserText
	}

	return ParsedChat{
		SystemPrompt: system,
		Turns:        turns,
		UserText:     userText,
		ToolResults:  toolResults,
	}
}

// RunPayload is a framed-ready AgentClientMessage plus local blob store for KV.
type RunPayload struct {
	RequestBytes []byte
	BlobStore    map[string][]byte // hex(blobID) → bytes
	Conversation string
	ModelID      string
	// Tools is echoed into exec request_context replies (may be empty).
	Tools []*cursorProto.McpToolDefinition
}

// BuildRunPayload builds an AgentClientMessage run_request (blob system prompt strategy).
func BuildRunPayload(modelID string, parsed ParsedChat) (*RunPayload, error) {
	if strings.TrimSpace(parsed.UserText) == "" {
		return nil, fmt.Errorf("chat request missing user message")
	}
	wireID := ResolveModelID(modelID)
	blobStore := map[string][]byte{}

	turnBytes := make([][]byte, 0, len(parsed.Turns))
	for _, turn := range parsed.Turns {
		userMsg := &cursorProto.UserMessage{
			Text:      turn.UserText,
			MessageId: newUUID(),
		}
		userMsgBytes, err := proto.Marshal(userMsg)
		if err != nil {
			return nil, err
		}
		var stepBytes [][]byte
		if turn.AssistantText != "" {
			step := &cursorProto.ConversationStep{
				Message: &cursorProto.ConversationStep_AssistantMessage{
					AssistantMessage: &cursorProto.AssistantMessage{Text: turn.AssistantText},
				},
			}
			b, err := proto.Marshal(step)
			if err != nil {
				return nil, err
			}
			stepBytes = append(stepBytes, b)
		}
		agentTurn := &cursorProto.AgentConversationTurnStructure{
			UserMessage: userMsgBytes,
			Steps:       stepBytes,
		}
		turnStruct := &cursorProto.ConversationTurnStructure{
			Turn: &cursorProto.ConversationTurnStructure_AgentConversationTurn{
				AgentConversationTurn: agentTurn,
			},
		}
		tb, err := proto.Marshal(turnStruct)
		if err != nil {
			return nil, err
		}
		turnBytes = append(turnBytes, tb)
	}

	systemJSON, err := json.Marshal(map[string]string{
		"role":    "system",
		"content": parsed.SystemPrompt,
	})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(systemJSON)
	blobStore[hex.EncodeToString(sum[:])] = append([]byte(nil), systemJSON...)

	convID := newUUID()
	state := &cursorProto.ConversationStateStructure{
		RootPromptMessagesJson: [][]byte{sum[:]},
		Turns:                  turnBytes,
		Todos:                  nil,
		PendingToolCalls:       nil,
		PreviousWorkspaceUris:  nil,
		FileStates:             map[string][]byte{},
		FileStatesV2:           map[string]*cursorProto.FileStateStructure{},
		SummaryArchives:        nil,
		TurnTimings:            nil,
		SubagentStates:         map[string]*cursorProto.SubagentPersistedState{},
		SelfSummaryCount:       0,
		ReadPaths:              nil,
	}

	runReq := &cursorProto.AgentRunRequest{
		ConversationState: state,
		Action: &cursorProto.ConversationAction{
			Action: &cursorProto.ConversationAction_UserMessageAction{
				UserMessageAction: &cursorProto.UserMessageAction{
					UserMessage: &cursorProto.UserMessage{
						Text:      parsed.UserText,
						MessageId: newUUID(),
					},
				},
			},
		},
		ModelDetails: &cursorProto.ModelDetails{
			ModelId:        wireID,
			DisplayModelId: wireID,
			DisplayName:    wireID,
		},
		ConversationId: proto.String(convID),
	}

	clientMsg := &cursorProto.AgentClientMessage{
		Message: &cursorProto.AgentClientMessage_RunRequest{RunRequest: runReq},
	}
	reqBytes, err := proto.Marshal(clientMsg)
	if err != nil {
		return nil, err
	}
	return &RunPayload{
		RequestBytes: reqBytes,
		BlobStore:    blobStore,
		Conversation: convID,
		ModelID:      wireID,
	}, nil
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
