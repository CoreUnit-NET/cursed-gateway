package completion_api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

// CompletionsRequest is the legacy OpenAI text completions shape.
type CompletionsRequest struct {
	Model         string          `json:"model"`
	Prompt        json.RawMessage `json:"prompt"`
	Stream        bool            `json:"stream"`
	StreamOptions *StreamOptions  `json:"stream_options,omitempty"`
	N             *int            `json:"n,omitempty"`
}

func (r *CompletionsRequest) UnmarshalJSON(data []byte) error {
	type plain CompletionsRequest
	var aux struct {
		plain
		Stream json.RawMessage `json:"stream"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*r = CompletionsRequest(aux.plain)
	return parseBoolish(aux.Stream, &r.Stream)
}

func (h *Handler) handleCompletions(w http.ResponseWriter, r *http.Request) {
	var req CompletionsRequest
	if err := readJSONBody(r, h.Server.maxBody(), &req); err != nil {
		writeJSONBodyError(h.Server, w, r, err)
		return
	}
	if err := validateN(req.N); err != nil {
		h.Server.writeAPIError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	prompt, err := parseCompletionsPrompt(req.Prompt)
	if err != nil {
		h.Server.writeAPIError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	chat := ChatCompletionRequest{
		Model: req.Model,
		Messages: []cursor_api_sdk.ChatMessage{
			{Role: "user", Content: prompt},
		},
		Stream:        req.Stream,
		StreamOptions: req.StreamOptions,
		N:             req.N,
	}
	if chat.Stream {
		h.streamChat(w, r, chat, envelopeCompletions)
		return
	}
	h.nonStreamChat(w, r, chat, envelopeCompletions)
}

func parseCompletionsPrompt(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", fmt.Errorf("prompt is required")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if strings.TrimSpace(s) == "" {
			return "", fmt.Errorf("prompt is required")
		}
		return s, nil
	}
	var parts []string
	if err := json.Unmarshal(raw, &parts); err == nil {
		if len(parts) == 0 {
			return "", fmt.Errorf("prompt is required")
		}
		joined := strings.Join(parts, "\n")
		if strings.TrimSpace(joined) == "" {
			return "", fmt.Errorf("prompt is required")
		}
		return joined, nil
	}
	return "", fmt.Errorf("prompt must be a string or array of strings")
}
