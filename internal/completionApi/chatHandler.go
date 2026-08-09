package completion_api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

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
	payload, err := cursor_api_sdk.BuildRunPayload(req.Model, parsed)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	dw := newDelayedWriter(w)
	var text string
	err = h.Server.withAccess(ctx, func(access string) error {
		t, err := h.Server.API.CollectText(ctx, access, payload)
		if err != nil {
			return err
		}
		text = t
		return nil
	})
	if err != nil {
		writeAPIError(dw, http.StatusBadGateway, err.Error())
		_ = dw.Commit()
		return
	}

	stop := "stop"
	resp := chatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   payload.ModelID,
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

func (h *Handler) streamChat(w http.ResponseWriter, r *http.Request, req ChatCompletionRequest) {
	ctx := r.Context()
	parsed := cursor_api_sdk.ParseChatMessages(req.Messages)
	payload, err := cursor_api_sdk.BuildRunPayload(req.Model, parsed)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	dw := newDelayedWriter(w)
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	model := payload.ModelID
	committed := false

	err = h.Server.withAccess(ctx, func(access string) error {
		ch, err := h.Server.API.RunChat(ctx, access, payload)
		if err != nil {
			return err
		}

		thinkingOpen := false
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
		for ev := range ch {
			if ev.Err != nil {
				if !committed {
					return ev.Err
				}
				h.Server.log().Warn("stream error after commit", "err", ev.Err)
				return nil
			}
			if ev.TurnEnded {
				if err := commitSSE(); err != nil {
					return err
				}
				if thinkingOpen {
					if err := writeSSEChunk(dw, id, created, model, "</think>", false); err != nil {
						return err
					}
					flusher.Flush()
				}
				if err := writeSSEChunk(dw, id, created, model, "", true); err != nil {
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
			if err := writeSSEChunk(dw, id, created, model, chunk, false); err != nil {
				return err
			}
			flusher.Flush()
		}
		if !committed {
			return cursor_api_sdk.ErrIncompleteRun
		}
		_, _ = dw.Write([]byte("data: [DONE]\n\n"))
		dw.Flush()
		return nil
	})
	if err != nil {
		if !committed {
			writeAPIError(dw, http.StatusBadGateway, err.Error())
			_ = dw.Commit()
			return
		}
	}
}

func writeSSEChunk(w http.ResponseWriter, id string, created int64, model, content string, finish bool) error {
	var finishReason *string
	delta := &chatDelta{}
	if finish {
		stop := "stop"
		finishReason = &stop
	} else {
		delta.Content = content
	}
	payload := chatCompletionResponse{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []chatCompletionChoice{{
			Index:        0,
			Delta:        delta,
			FinishReason: finishReason,
		}},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", b)
	return err
}
