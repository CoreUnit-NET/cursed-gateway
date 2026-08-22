package completion_api

import (
	"fmt"
	"net/http"
	"strings"

	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

// CompletionsRequest is the legacy OpenAI text completions shape.
type CompletionsRequest struct {
	Model  string `json:"model"`
	Prompt any    `json:"prompt"`
	Stream bool   `json:"stream"`
}

func (h *Handler) handleCompletions(w http.ResponseWriter, r *http.Request) {
	var req CompletionsRequest
	if err := readJSONBody(r, h.Server.maxBody(), &req); err != nil {
		writeJSONBodyError(h.Server, w, r, err)
		return
	}
	chat := ChatCompletionRequest{
		Model: req.Model,
		Messages: []cursor_api_sdk.ChatMessage{
			{Role: "user", Content: promptToString(req.Prompt)},
		},
		Stream: req.Stream,
	}
	if chat.Stream {
		h.streamChat(w, r, chat, envelopeCompletions)
		return
	}
	h.nonStreamChat(w, r, chat, envelopeCompletions)
}

func promptToString(prompt any) string {
	switch v := prompt.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, p := range v {
			parts = append(parts, fmt.Sprint(p))
		}
		return strings.Join(parts, "\n")
	default:
		if prompt == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
}
