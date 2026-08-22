package completion_api

import cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"

// ChatCompletionRequest is the OpenAI chat completions body we accept.
type ChatCompletionRequest struct {
	Model    string                         `json:"model"`
	Messages []cursor_api_sdk.ChatMessage   `json:"messages"`
	Stream   bool                           `json:"stream"`
	Tools    []cursor_api_sdk.OpenAIToolDef `json:"tools,omitempty"`
	// Sticky identity fields (otto conversation/identity.ts) — first non-empty wins.
	ConversationID string         `json:"conversation_id,omitempty"`
	ThreadID       string         `json:"thread_id,omitempty"`
	SessionID      string         `json:"session_id,omitempty"`
	User           string         `json:"user,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

func (r ChatCompletionRequest) conversationIdentity() cursor_api_sdk.ConversationIdentity {
	return cursor_api_sdk.ConversationIdentity{
		ConversationID: r.ConversationID,
		ThreadID:       r.ThreadID,
		SessionID:      r.SessionID,
		User:           r.User,
		Metadata:       r.Metadata,
	}
}

type modelListResponse struct {
	Object string        `json:"object"`
	Data   []modelObject `json:"data"`
}

type modelObject struct {
	ID      string  `json:"id"`
	Object  string  `json:"object"`
	Created int64   `json:"created"`
	OwnedBy string  `json:"owned_by"`
	Name    string  `json:"name,omitempty"`
	Root    string  `json:"root,omitempty"`
	Parent  *string `json:"parent"`
}

type chatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []chatCompletionChoice `json:"choices"`
	Usage   *usage                 `json:"usage,omitempty"`
}

type chatCompletionChoice struct {
	Index        int        `json:"index"`
	Message      *chatMsg   `json:"message,omitempty"`
	Delta        *chatDelta `json:"delta,omitempty"`
	FinishReason *string    `json:"finish_reason"`
}

type chatMsg struct {
	Role      string                          `json:"role"`
	Content   *string                         `json:"content"` // null when tool_calls-only (OpenAI shape)
	ToolCalls []cursor_api_sdk.OpenAIToolCall `json:"tool_calls,omitempty"`
}

type chatDelta struct {
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []streamToolCall `json:"tool_calls,omitempty"`
}

// Legacy OpenAI text completions response (POST /v1/completions).
type textCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []textCompletionChoice `json:"choices"`
	Usage   *usage                 `json:"usage,omitempty"`
}

type textCompletionChoice struct {
	Index        int     `json:"index"`
	Text         string  `json:"text"`
	FinishReason *string `json:"finish_reason"`
}

// apiEnvelope selects chat.completion vs text_completion response shape.
type apiEnvelope int

const (
	envelopeChat apiEnvelope = iota
	envelopeCompletions
)

type streamToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function *struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function,omitempty"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type errorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}
