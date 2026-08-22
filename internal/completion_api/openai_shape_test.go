package completion_api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

// scriptedTextAPI returns a one-shot RunControl that emits text then turn_ended.
type scriptedTextAPI struct {
	recAPI
	text string
}

func (a *scriptedTextAPI) StartRun(ctx context.Context, accessToken string, payload *cursor_api_sdk.RunPayload, bridgeTools bool) (*cursor_api_sdk.RunControl, error) {
	text := a.text
	if text == "" {
		text = "hello"
	}
	ch := make(chan cursor_api_sdk.StreamEvent, 2)
	ch <- cursor_api_sdk.StreamEvent{Text: text}
	ch <- cursor_api_sdk.StreamEvent{TurnEnded: true}
	close(ch)
	return &cursor_api_sdk.RunControl{Events: ch}, nil
}

func TestBridgeMissErrorMentionsStickyIDs(t *testing.T) {
	h := &Handler{Server: &Server{Pool: &recPool{}, API: &recAPI{}}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	payload := map[string]any{
		"model":           "composer-2.5",
		"conversation_id": "conv-sticky-1",
		"messages": []map[string]string{
			{"role": "tool", "tool_call_id": "call_1", "content": `{"ok":true}`},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(srv.URL+"/ai/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", res.StatusCode, raw)
	}
	msg := string(raw)
	if !strings.Contains(msg, "no active tool session") {
		t.Fatalf("missing bridge miss text: %s", msg)
	}
	if !strings.Contains(msg, "conversation_id") || !strings.Contains(msg, "thread_id") {
		t.Fatalf("bridge miss should mention sticky ids: %s", msg)
	}
	if !strings.Contains(msg, "sticky id") {
		t.Fatalf("bridge miss should mention sticky id: %s", msg)
	}
}

func TestCompletionsHTTPSuccessTextCompletion(t *testing.T) {
	h := &Handler{Server: &Server{Pool: &recPool{}, API: &scriptedTextAPI{text: "hello from completions"}}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	for _, path := range []string{"/ai/v1/completions", "/v1/completions", "/completions"} {
		body, err := json.Marshal(map[string]any{
			"model":  "composer-2.5",
			"prompt": "say hi",
		})
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusOK {
			t.Fatalf("POST %s status=%d body=%s", path, res.StatusCode, raw)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("POST %s json: %v body=%s", path, err, raw)
		}
		if out["object"] != "text_completion" {
			t.Fatalf("POST %s object=%#v, want text_completion", path, out["object"])
		}
		id, _ := out["id"].(string)
		if !strings.HasPrefix(id, "cmpl-") {
			t.Fatalf("POST %s id=%q, want cmpl- prefix", path, id)
		}
		choices, _ := out["choices"].([]any)
		if len(choices) != 1 {
			t.Fatalf("POST %s choices=%#v", path, out["choices"])
		}
		choice, _ := choices[0].(map[string]any)
		if choice["text"] != "hello from completions" {
			t.Fatalf("POST %s text=%#v", path, choice["text"])
		}
		if _, ok := choice["message"]; ok {
			t.Fatalf("POST %s must not use chat message field: %#v", path, choice)
		}
	}
}

func TestChatHTTPSuccessChatCompletion(t *testing.T) {
	h := &Handler{Server: &Server{Pool: &recPool{}, API: &scriptedTextAPI{text: "hello from chat"}}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, err := json.Marshal(map[string]any{
		"model": "composer-2.5",
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(srv.URL+"/ai/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.StatusCode, raw)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out["object"] != "chat.completion" {
		t.Fatalf("object=%#v", out["object"])
	}
	id, _ := out["id"].(string)
	if !strings.HasPrefix(id, "chatcmpl-") {
		t.Fatalf("id=%q", id)
	}
	msg := out["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "hello from chat" {
		t.Fatalf("content=%#v", msg["content"])
	}
}

func TestCompletionsHTTPStreamTextCompletion(t *testing.T) {
	h := &Handler{Server: &Server{Pool: &recPool{}, API: &scriptedTextAPI{text: "streamed"}}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, err := json.Marshal(map[string]any{
		"model":  "composer-2.5",
		"prompt": "say hi",
		"stream": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(srv.URL+"/ai/v1/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.StatusCode, raw)
	}
	s := string(raw)
	if !strings.Contains(s, `"object":"text_completion"`) {
		t.Fatalf("missing text_completion in SSE: %s", s)
	}
	if !strings.Contains(s, `"text":"streamed"`) {
		t.Fatalf("missing streamed text: %s", s)
	}
	if !strings.Contains(s, "data: [DONE]") {
		t.Fatalf("missing DONE: %s", s)
	}
	if strings.Contains(s, "chat.completion") {
		t.Fatalf("stream must not use chat.completion: %s", s)
	}
}

func TestWriteNonStreamToolCallsContentNull(t *testing.T) {
	rec := httptest.NewRecorder()
	dw := newDelayedWriter(rec)
	writeNonStreamToolCalls(dw, "chatcmpl-1", 1, "m", "", []cursor_api_sdk.PendingExec{{
		ToolCallID:  "call_1",
		ToolName:    "lookup",
		DecodedArgs: `{"q":"x"}`,
	}}, cursor_api_sdk.Usage{})

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	choices, _ := raw["choices"].([]any)
	if len(choices) != 1 {
		t.Fatalf("choices = %#v", choices)
	}
	msg, _ := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != nil {
		t.Fatalf("expected content null, got %#v", msg["content"])
	}
	if msg["role"] != "assistant" {
		t.Fatalf("role = %#v", msg["role"])
	}
}

func TestWriteNonStreamTextEnvelopes(t *testing.T) {
	t.Run("chat", func(t *testing.T) {
		rec := httptest.NewRecorder()
		dw := newDelayedWriter(rec)
		writeNonStreamText(dw, "chatcmpl-1", 1, "m", "hi", cursor_api_sdk.Usage{}, envelopeChat)
		var raw map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		if raw["object"] != "chat.completion" {
			t.Fatalf("object = %#v", raw["object"])
		}
		msg := raw["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
		if msg["content"] != "hi" {
			t.Fatalf("content = %#v", msg["content"])
		}
	})
	t.Run("completions", func(t *testing.T) {
		rec := httptest.NewRecorder()
		dw := newDelayedWriter(rec)
		writeNonStreamText(dw, "cmpl-1", 1, "m", "hi", cursor_api_sdk.Usage{}, envelopeCompletions)
		var raw map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		if raw["object"] != "text_completion" {
			t.Fatalf("object = %#v", raw["object"])
		}
		choice := raw["choices"].([]any)[0].(map[string]any)
		if choice["text"] != "hi" {
			t.Fatalf("text = %#v", choice["text"])
		}
	})
}

func TestWriteSSERoleAndChatContent(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := writeSSERole(rec, "chatcmpl-1", 1, "m"); err != nil {
		t.Fatal(err)
	}
	if err := writeSSEContent(rec, "chatcmpl-1", 1, "m", "hello", envelopeChat); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"role":"assistant"`) {
		t.Fatalf("missing role chunk: %s", body)
	}
	if !strings.Contains(body, `"content":"hello"`) {
		t.Fatalf("missing content chunk: %s", body)
	}
	if !strings.Contains(body, `"object":"chat.completion.chunk"`) {
		t.Fatalf("missing chat chunk object: %s", body)
	}
}

func TestWriteSSECompletionsContent(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := writeSSEContent(rec, "cmpl-1", 1, "m", "hello", envelopeCompletions); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"object":"text_completion"`) {
		t.Fatalf("missing text_completion object: %s", body)
	}
	if !strings.Contains(body, `"text":"hello"`) {
		t.Fatalf("missing text: %s", body)
	}
}

func TestConsumeRunSkipsThinking(t *testing.T) {
	ch := make(chan cursor_api_sdk.StreamEvent, 4)
	ch <- cursor_api_sdk.StreamEvent{Text: "secret plan", Thinking: true}
	ch <- cursor_api_sdk.StreamEvent{Text: "visible"}
	ch <- cursor_api_sdk.StreamEvent{TurnEnded: true}
	close(ch)
	rc := &cursor_api_sdk.RunControl{Events: ch}

	text, calls, err := consumeRun(rc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if text != "visible" {
		t.Fatalf("text = %q", text)
	}
	if len(calls) != 0 {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestStreamFromRunSkipsThinking(t *testing.T) {
	ch := make(chan cursor_api_sdk.StreamEvent, 4)
	ch <- cursor_api_sdk.StreamEvent{Text: "secret plan", Thinking: true}
	ch <- cursor_api_sdk.StreamEvent{Text: "visible"}
	ch <- cursor_api_sdk.StreamEvent{TurnEnded: true}
	close(ch)
	rc := &cursor_api_sdk.RunControl{Events: ch}

	rec := httptest.NewRecorder()
	dw := newDelayedWriter(rec)
	committed := false
	commitSSE := func() error {
		if committed {
			return nil
		}
		if err := dw.Commit(); err != nil {
			return err
		}
		committed = true
		return nil
	}
	h := &Handler{Server: &Server{}}
	if err := streamFromRun(dw, rec, commitSSE, &committed, "chatcmpl-1", 1, "m", "bridge", rc, h, "", "", envelopeChat); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret plan") {
		t.Fatalf("thinking leaked into SSE: %s", body)
	}
	if !strings.Contains(body, `"content":"visible"`) {
		t.Fatalf("missing visible content: %s", body)
	}
	if !strings.Contains(body, `"role":"assistant"`) {
		t.Fatalf("missing role chunk: %s", body)
	}
}

func TestStreamFromRunIncompleteEmitsDONE(t *testing.T) {
	// Content flushed, then channel closes without TurnEnded — must still finish SSE.
	ch := make(chan cursor_api_sdk.StreamEvent, 1)
	ch <- cursor_api_sdk.StreamEvent{Text: "partial"}
	close(ch)
	rc := &cursor_api_sdk.RunControl{Events: ch}

	rec := httptest.NewRecorder()
	dw := newDelayedWriter(rec)
	committed := false
	commitSSE := func() error {
		if committed {
			return nil
		}
		if err := dw.Commit(); err != nil {
			return err
		}
		committed = true
		return nil
	}
	h := &Handler{Server: &Server{}}
	if err := streamFromRun(dw, rec, commitSSE, &committed, "chatcmpl-1", 1, "m", "bridge", rc, h, "", "", envelopeChat); err != nil {
		t.Fatalf("expected nil after committed incomplete close, got %v", err)
	}
	if !committed {
		t.Fatal("expected SSE commit")
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"content":"partial"`) {
		t.Fatalf("missing partial content: %s", body)
	}
	if !strings.Contains(body, `"finish_reason":"stop"`) {
		t.Fatalf("missing finish_reason stop: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("missing [DONE]: %s", body)
	}
}

func TestStreamFromRunErrorAfterCommitEmitsDONE(t *testing.T) {
	ch := make(chan cursor_api_sdk.StreamEvent, 2)
	ch <- cursor_api_sdk.StreamEvent{Text: "partial"}
	ch <- cursor_api_sdk.StreamEvent{Err: errors.New("upstream boom")}
	close(ch)
	rc := &cursor_api_sdk.RunControl{Events: ch}

	rec := httptest.NewRecorder()
	dw := newDelayedWriter(rec)
	committed := false
	commitSSE := func() error {
		if committed {
			return nil
		}
		if err := dw.Commit(); err != nil {
			return err
		}
		committed = true
		return nil
	}
	h := &Handler{Server: &Server{}}
	if err := streamFromRun(dw, rec, commitSSE, &committed, "chatcmpl-1", 1, "m", "bridge", rc, h, "", "", envelopeChat); err != nil {
		t.Fatalf("expected nil after committed stream error, got %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"content":"partial"`) {
		t.Fatalf("missing partial content: %s", body)
	}
	if !strings.Contains(body, `"finish_reason":"stop"`) {
		t.Fatalf("missing finish_reason stop: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("missing [DONE]: %s", body)
	}
}

func TestConsumeRunThinkingBeforeToolCalls(t *testing.T) {
	ch := make(chan cursor_api_sdk.StreamEvent, 4)
	ch <- cursor_api_sdk.StreamEvent{Text: "thinking…", Thinking: true}
	ch <- cursor_api_sdk.StreamEvent{ToolCall: &cursor_api_sdk.PendingExec{
		ToolCallID:  "c1",
		ToolName:    "lookup",
		DecodedArgs: `{}`,
	}}
	close(ch)
	rc := &cursor_api_sdk.RunControl{Events: ch}

	text, calls, err := consumeRun(rc, 0)
	if err != nil {
		t.Fatal(err)
	}
	if text != "" {
		t.Fatalf("expected empty content before tool_calls, got %q", text)
	}
	if len(calls) != 1 || calls[0].ToolCallID != "c1" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestWriteNonStreamToolCallsKeepsVisibleText(t *testing.T) {
	rec := httptest.NewRecorder()
	dw := newDelayedWriter(rec)
	writeNonStreamToolCalls(dw, "chatcmpl-1", time.Now().Unix(), "m", "before tools", []cursor_api_sdk.PendingExec{{
		ToolCallID:  "call_1",
		ToolName:    "lookup",
		DecodedArgs: `{}`,
	}}, cursor_api_sdk.Usage{})
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"content":"before tools"`)) {
		t.Fatalf("expected visible content kept: %s", rec.Body.String())
	}
}
