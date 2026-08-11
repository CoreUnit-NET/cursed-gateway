package completion_api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

const toolCallGrace = 150 * time.Millisecond

func (h *Handler) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req ChatCompletionRequest
	if err := readJSONBody(r, h.Server.maxBody(), &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Messages) == 0 {
		writeAPIError(w, http.StatusBadRequest, "messages is required")
		return
	}
	if req.Stream {
		h.streamChat(w, r, req)
		return
	}
	h.nonStreamChat(w, r, req)
}

func (h *Handler) nonStreamChat(w http.ResponseWriter, r *http.Request, req ChatCompletionRequest) {
	ctx := r.Context()
	parsed := cursor_api_sdk.ParseChatMessages(req.Messages)
	if strings.TrimSpace(parsed.UserText) == "" && len(parsed.ToolResults) == 0 {
		writeAPIError(w, http.StatusBadRequest, "No user message found")
		return
	}

	bridgeKey := cursor_api_sdk.DeriveBridgeKey(req.Model, req.Messages)
	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	dw := newDelayedWriter(w)

	if len(parsed.ToolResults) > 0 {
		br := h.Server.bridges().take(bridgeKey)
		if br == nil || br.RC == nil {
			writeAPIError(w, http.StatusBadRequest, "no active tool session for this conversation")
			return
		}
		if err := br.RC.SubmitMcpResults(parsed.ToolResults); err != nil {
			br.RC.Close()
			writeAPIError(dw, http.StatusBadGateway, err.Error())
			_ = dw.Commit()
			return
		}
		model := br.ModelID
		if model == "" {
			model = cursor_api_sdk.ResolveModelID(req.Model)
		}
		text, calls, err := consumeRun(br.RC, toolCallGrace)
		if err != nil {
			br.RC.Close()
			writeAPIError(dw, http.StatusBadGateway, err.Error())
			_ = dw.Commit()
			return
		}
		if len(calls) > 0 {
			h.Server.bridges().park(bridgeKey, br.RC, model)
			writeNonStreamToolCalls(dw, id, created, model, text, calls)
			return
		}
		br.RC.Close()
		writeNonStreamText(dw, id, created, model, text)
		return
	}

	mcpTools, err := cursor_api_sdk.BuildMcpToolDefinitions(req.Tools)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload, err := cursor_api_sdk.BuildRunPayload(req.Model, parsed)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload.Tools = mcpTools
	h.Server.bridges().drop(bridgeKey)

	bridgeTools := len(mcpTools) > 0
	var text string
	var calls []cursor_api_sdk.PendingExec
	var parkedRC *cursor_api_sdk.RunControl
	err = h.Server.withAccess(ctx, func(access string) error {
		rc, err := h.Server.API.StartRun(ctx, access, payload, bridgeTools)
		if err != nil {
			return err
		}
		t, c, err := consumeRun(rc, toolCallGrace)
		if err != nil {
			rc.Close()
			return err
		}
		text, calls = t, c
		if len(calls) > 0 {
			parkedRC = rc
			return nil
		}
		rc.Close()
		return nil
	})
	if err != nil {
		writeAPIError(dw, http.StatusBadGateway, err.Error())
		_ = dw.Commit()
		return
	}
	if len(calls) > 0 && parkedRC != nil {
		h.Server.bridges().park(bridgeKey, parkedRC, payload.ModelID)
		writeNonStreamToolCalls(dw, id, created, payload.ModelID, text, calls)
		return
	}
	writeNonStreamText(dw, id, created, payload.ModelID, text)
}

func (h *Handler) streamChat(w http.ResponseWriter, r *http.Request, req ChatCompletionRequest) {
	ctx := r.Context()
	parsed := cursor_api_sdk.ParseChatMessages(req.Messages)
	if strings.TrimSpace(parsed.UserText) == "" && len(parsed.ToolResults) == 0 {
		writeAPIError(w, http.StatusBadRequest, "No user message found")
		return
	}

	bridgeKey := cursor_api_sdk.DeriveBridgeKey(req.Model, req.Messages)
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	dw := newDelayedWriter(w)
	committed := false

	commitSSE := func() error {
		if committed {
			return nil
		}
		dw.Header().Set("Content-Type", "text/event-stream")
		dw.Header().Set("Cache-Control", "no-cache")
		dw.Header().Set("Connection", "keep-alive")
		if err := dw.Commit(); err != nil {
			return err
		}
		committed = true
		return nil
	}

	if len(parsed.ToolResults) > 0 {
		br := h.Server.bridges().take(bridgeKey)
		if br == nil || br.RC == nil {
			writeAPIError(w, http.StatusBadRequest, "no active tool session for this conversation")
			return
		}
		model := br.ModelID
		if model == "" {
			model = cursor_api_sdk.ResolveModelID(req.Model)
		}
		if err := br.RC.SubmitMcpResults(parsed.ToolResults); err != nil {
			br.RC.Close()
			writeAPIError(dw, http.StatusBadGateway, err.Error())
			_ = dw.Commit()
			return
		}
		if err := streamFromRun(dw, flusher, commitSSE, &committed, id, created, model, bridgeKey, br.RC, h); err != nil && !committed {
			br.RC.Close()
			writeAPIError(dw, http.StatusBadGateway, err.Error())
			_ = dw.Commit()
		}
		return
	}

	mcpTools, err := cursor_api_sdk.BuildMcpToolDefinitions(req.Tools)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload, err := cursor_api_sdk.BuildRunPayload(req.Model, parsed)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	payload.Tools = mcpTools
	h.Server.bridges().drop(bridgeKey)

	bridgeTools := len(mcpTools) > 0
	err = h.Server.withAccess(ctx, func(access string) error {
		rc, err := h.Server.API.StartRun(ctx, access, payload, bridgeTools)
		if err != nil {
			return err
		}
		return streamFromRun(dw, flusher, commitSSE, &committed, id, created, payload.ModelID, bridgeKey, rc, h)
	})
	if err != nil && !committed {
		writeAPIError(dw, http.StatusBadGateway, err.Error())
		_ = dw.Commit()
	}
}

func streamFromRun(
	dw *delayedWriter,
	flusher http.Flusher,
	commitSSE func() error,
	committed *bool,
	id string,
	created int64,
	model string,
	bridgeKey string,
	rc *cursor_api_sdk.RunControl,
	h *Handler,
) error {
	thinkingOpen := false
	toolIndex := 0
	parked := false
	defer func() {
		if !parked && rc != nil {
			rc.Close()
		}
	}()

	for {
		ev, ok := rc.Recv()
		if !ok {
			break
		}
		if ev.Err != nil {
			if !*committed {
				return ev.Err
			}
			h.Server.log().Warn("stream error after commit", "err", ev.Err)
			return nil
		}
		if ev.ToolCall != nil {
			calls := collectMoreToolCalls(rc, *ev.ToolCall, toolCallGrace)
			if err := commitSSE(); err != nil {
				return err
			}
			if thinkingOpen {
				if err := writeSSEContent(dw, id, created, model, "</think>"); err != nil {
					return err
				}
				flusher.Flush()
				thinkingOpen = false
			}
			for _, pe := range calls {
				if err := writeSSEToolCall(dw, id, created, model, toolIndex, pe); err != nil {
					return err
				}
				toolIndex++
				flusher.Flush()
			}
			if err := writeSSEFinish(dw, id, created, model, "tool_calls"); err != nil {
				return err
			}
			_, _ = dw.Write([]byte("data: [DONE]\n\n"))
			dw.Flush()
			h.Server.bridges().park(bridgeKey, rc, model)
			parked = true
			return nil
		}
		if ev.TurnEnded {
			if err := commitSSE(); err != nil {
				return err
			}
			if thinkingOpen {
				if err := writeSSEContent(dw, id, created, model, "</think>"); err != nil {
					return err
				}
				flusher.Flush()
				thinkingOpen = false
			}
			if err := writeSSEFinish(dw, id, created, model, "stop"); err != nil {
				return err
			}
			_, _ = dw.Write([]byte("data: [DONE]\n\n"))
			dw.Flush()
			return nil
		}
		if ev.Text == "" {
			continue
		}
		chunk := ev.Text
		if ev.Thinking {
			if !thinkingOpen {
				chunk = "<think>" + chunk
				thinkingOpen = true
			}
		} else if thinkingOpen {
			chunk = "</think>" + chunk
			thinkingOpen = false
		}
		if err := commitSSE(); err != nil {
			return err
		}
		if err := writeSSEContent(dw, id, created, model, chunk); err != nil {
			return err
		}
		flusher.Flush()
	}
	if !*committed {
		return cursor_api_sdk.ErrIncompleteRun
	}
	_, _ = dw.Write([]byte("data: [DONE]\n\n"))
	dw.Flush()
	return nil
}

func consumeRun(rc *cursor_api_sdk.RunControl, grace time.Duration) (string, []cursor_api_sdk.PendingExec, error) {
	var b strings.Builder
	thinkingOpen := false
	for {
		ev, ok := rc.Recv()
		if !ok {
			break
		}
		if ev.Err != nil {
			return b.String(), nil, ev.Err
		}
		if ev.ToolCall != nil {
			if thinkingOpen {
				b.WriteString("</think>")
				thinkingOpen = false
			}
			calls := collectMoreToolCalls(rc, *ev.ToolCall, grace)
			return b.String(), calls, nil
		}
		if ev.TurnEnded {
			if thinkingOpen {
				b.WriteString("</think>")
			}
			return b.String(), nil, nil
		}
		if ev.Text == "" {
			continue
		}
		if ev.Thinking {
			if !thinkingOpen {
				b.WriteString("<think>")
				thinkingOpen = true
			}
			b.WriteString(ev.Text)
			continue
		}
		if thinkingOpen {
			b.WriteString("</think>")
			thinkingOpen = false
		}
		b.WriteString(ev.Text)
	}
	return b.String(), nil, cursor_api_sdk.ErrIncompleteRun
}

// collectMoreToolCalls waits briefly for parallel mcpArgs, then drains buffered ToolCall events.
func collectMoreToolCalls(rc *cursor_api_sdk.RunControl, first cursor_api_sdk.PendingExec, grace time.Duration) []cursor_api_sdk.PendingExec {
	out := []cursor_api_sdk.PendingExec{first}
	if grace > 0 {
		time.Sleep(grace)
	}
	seen := map[string]struct{}{first.ToolCallID: {}}
	for _, pe := range rc.Pending() {
		if _, ok := seen[pe.ToolCallID]; ok {
			continue
		}
		seen[pe.ToolCallID] = struct{}{}
		out = append(out, pe)
	}
	// Drop matching ToolCall events already queued so resume does not re-emit them.
	// Non-tool events are unread so text/turn-end is not lost.
	for {
		ev, ok := rc.TryRecv()
		if !ok {
			break
		}
		if ev.ToolCall != nil {
			if _, exists := seen[ev.ToolCall.ToolCallID]; !exists {
				seen[ev.ToolCall.ToolCallID] = struct{}{}
				out = append(out, *ev.ToolCall)
			}
			continue
		}
		rc.Unread(ev)
		break
	}
	return out
}

func writeNonStreamText(dw *delayedWriter, id string, created int64, model, text string) {
	stop := "stop"
	resp := chatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []chatCompletionChoice{{
			Index:        0,
			Message:      &chatMsg{Role: "assistant", Content: text},
			FinishReason: &stop,
		}},
		Usage: usage{},
	}
	dw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(dw).Encode(resp)
	_ = dw.Commit()
}

func writeNonStreamToolCalls(dw *delayedWriter, id string, created int64, model, text string, calls []cursor_api_sdk.PendingExec) {
	reason := "tool_calls"
	toolCalls := make([]cursor_api_sdk.OpenAIToolCall, 0, len(calls))
	for _, pe := range calls {
		tc := cursor_api_sdk.OpenAIToolCall{ID: pe.ToolCallID, Type: "function"}
		tc.Function.Name = pe.ToolName
		tc.Function.Arguments = pe.DecodedArgs
		toolCalls = append(toolCalls, tc)
	}
	resp := chatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []chatCompletionChoice{{
			Index: 0,
			Message: &chatMsg{
				Role:      "assistant",
				Content:   text,
				ToolCalls: toolCalls,
			},
			FinishReason: &reason,
		}},
		Usage: usage{},
	}
	dw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(dw).Encode(resp)
	_ = dw.Commit()
}

func writeSSEContent(w http.ResponseWriter, id string, created int64, model, content string) error {
	payload := chatCompletionResponse{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []chatCompletionChoice{{
			Index: 0,
			Delta: &chatDelta{Content: content},
		}},
	}
	return writeSSEData(w, payload)
}

func writeSSEToolCall(w http.ResponseWriter, id string, created int64, model string, index int, pe cursor_api_sdk.PendingExec) error {
	fn := &struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	}{Name: pe.ToolName, Arguments: pe.DecodedArgs}
	payload := chatCompletionResponse{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []chatCompletionChoice{{
			Index: 0,
			Delta: &chatDelta{
				ToolCalls: []streamToolCall{{
					Index:    index,
					ID:       pe.ToolCallID,
					Type:     "function",
					Function: fn,
				}},
			},
		}},
	}
	return writeSSEData(w, payload)
}

func writeSSEFinish(w http.ResponseWriter, id string, created int64, model, reason string) error {
	r := reason
	payload := chatCompletionResponse{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []chatCompletionChoice{{
			Index:        0,
			Delta:        &chatDelta{},
			FinishReason: &r,
		}},
	}
	return writeSSEData(w, payload)
}

func writeSSEData(w http.ResponseWriter, payload chatCompletionResponse) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", b)
	return err
}
