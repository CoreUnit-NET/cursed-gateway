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
	cursorProto "github.com/CoreUnit-NET/cursed-gateway/lib/cursorProto"
	"google.golang.org/protobuf/proto"
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

// captureResumeAPI records whether StartRun received a ResumeAction.
type captureResumeAPI struct {
	recAPI
	text      string
	sawResume bool
}

func (a *captureResumeAPI) StartRun(ctx context.Context, accessToken string, payload *cursor_api_sdk.RunPayload, bridgeTools bool) (*cursor_api_sdk.RunControl, error) {
	if payload != nil {
		var msg cursorProto.AgentClientMessage
		if err := proto.Unmarshal(payload.RequestBytes, &msg); err == nil {
			if run := msg.GetRunRequest(); run != nil && run.GetAction().GetResumeAction() != nil {
				a.sawResume = true
			}
		}
	}
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

func TestBridgeMissFallsBackToResumeAction(t *testing.T) {
	api := &captureResumeAPI{text: "resumed ok"}
	h := &Handler{Server: &Server{Pool: &recPool{}, API: api}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	payload := map[string]any{
		"model":           "composer-2.5",
		"conversation_id": "conv-sticky-1",
		"messages": []map[string]any{
			{"role": "user", "content": "lookup x"},
			{"role": "assistant", "content": nil, "tool_calls": []map[string]any{{
				"id":   "call_1",
				"type": "function",
				"function": map[string]any{
					"name":      "lookup",
					"arguments": `{"q":"x"}`,
				},
			}}},
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
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.StatusCode, raw)
	}
	if !api.sawResume {
		t.Fatal("expected StartRun with ResumeAction after bridge miss")
	}
	if !strings.Contains(string(raw), "resumed ok") {
		t.Fatalf("body=%s", raw)
	}
}

func TestBridgeMissToolOnlyWithoutHistoryStill400(t *testing.T) {
	h := &Handler{Server: &Server{Pool: &recPool{}, API: &recAPI{}}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	payload := map[string]any{
		"model": "composer-2.5",
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
	if !strings.Contains(string(raw), "No user message") {
		t.Fatalf("expected missing-user error, got %s", raw)
	}
}

type captureToolsAPI struct {
	scriptedTextAPI
	toolCount   int
	bridgeTools bool
}

func (a *captureToolsAPI) StartRun(ctx context.Context, accessToken string, payload *cursor_api_sdk.RunPayload, bridgeTools bool) (*cursor_api_sdk.RunControl, error) {
	a.bridgeTools = bridgeTools
	a.toolCount = 0
	if payload != nil {
		a.toolCount = len(payload.Tools)
	}
	return a.scriptedTextAPI.StartRun(ctx, accessToken, payload, bridgeTools)
}

func TestChatHTTPToolChoiceNoneClearsMCPTools(t *testing.T) {
	api := &captureToolsAPI{scriptedTextAPI: scriptedTextAPI{text: "no tools"}}
	h := &Handler{Server: &Server{Pool: &recPool{}, API: api}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, err := json.Marshal(map[string]any{
		"model":       "composer-2.5",
		"tool_choice": "none",
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
		"tools": []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name": "lookup",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		}},
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
	if api.toolCount != 0 || api.bridgeTools {
		t.Fatalf("tools=%d bridge=%t, want empty MCP defs", api.toolCount, api.bridgeTools)
	}
}

func TestModelsHTTPIncludesNameRootParent(t *testing.T) {
	h := &Handler{Server: &Server{Pool: &recPool{}, API: &recAPI{cache: []cursor_api_sdk.Model{{ID: "composer-2.5", Name: "Composer 2.5"}}}}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/ai/v1/models")
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
	var out modelListResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 1 {
		t.Fatalf("data=%#v", out.Data)
	}
	m := out.Data[0]
	if m.ID != "composer-2.5" || m.Name != "Composer 2.5" || m.Root != "composer-2.5" || m.Parent != nil {
		t.Fatalf("model=%#v", m)
	}
	if !bytes.Contains(raw, []byte(`"parent":null`)) {
		t.Fatalf("expected parent null in JSON: %s", raw)
	}
}

func TestChatHTTPPreservesAngleBrackets(t *testing.T) {
	h := &Handler{Server: &Server{Pool: &recPool{}, API: &scriptedTextAPI{text: `use <tag> & more`}}}
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
	if bytes.Contains(raw, []byte(`\u003c`)) {
		t.Fatalf("HTML-escaped content: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`use <tag> & more`)) {
		t.Fatalf("missing raw content: %s", raw)
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
	if err := streamFromRun(dw, rec, commitSSE, &committed, "chatcmpl-1", 1, "m", "bridge", rc, h, "", "", envelopeChat, false); err != nil {
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
	if err := streamFromRun(dw, rec, commitSSE, &committed, "chatcmpl-1", 1, "m", "bridge", rc, h, "", "", envelopeChat, false); err != nil {
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
	if err := streamFromRun(dw, rec, commitSSE, &committed, "chatcmpl-1", 1, "m", "bridge", rc, h, "", "", envelopeChat, false); err != nil {
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

// scriptedEventsAPI emits a fixed StreamEvent sequence from StartRun.
type scriptedEventsAPI struct {
	recAPI
	events []cursor_api_sdk.StreamEvent
}

func (a *scriptedEventsAPI) StartRun(ctx context.Context, accessToken string, payload *cursor_api_sdk.RunPayload, bridgeTools bool) (*cursor_api_sdk.RunControl, error) {
	ch := make(chan cursor_api_sdk.StreamEvent, len(a.events)+1)
	for _, ev := range a.events {
		ch <- ev
	}
	close(ch)
	return &cursor_api_sdk.RunControl{Events: ch}, nil
}

func TestChatHTTPStreamSkipsThinkingAndEmitsRole(t *testing.T) {
	h := &Handler{Server: &Server{Pool: &recPool{}, API: &scriptedEventsAPI{events: []cursor_api_sdk.StreamEvent{
		{Text: "secret plan", Thinking: true},
		{Text: "visible"},
		{TurnEnded: true},
	}}}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, err := json.Marshal(map[string]any{
		"model":  "composer-2.5",
		"stream": true,
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
	s := string(raw)
	if strings.Contains(s, "secret plan") {
		t.Fatalf("thinking leaked: %s", s)
	}
	if !strings.Contains(s, `"role":"assistant"`) {
		t.Fatalf("missing role chunk: %s", s)
	}
	if !strings.Contains(s, `"content":"visible"`) {
		t.Fatalf("missing visible content: %s", s)
	}
	if !strings.Contains(s, `"object":"chat.completion.chunk"`) {
		t.Fatalf("missing chat chunk object: %s", s)
	}
	if !strings.Contains(s, `"finish_reason":"stop"`) {
		t.Fatalf("missing finish_reason: %s", s)
	}
	if !strings.Contains(s, "data: [DONE]") {
		t.Fatalf("missing DONE: %s", s)
	}
}

func TestChatHTTPNonStreamSkipsThinking(t *testing.T) {
	h := &Handler{Server: &Server{Pool: &recPool{}, API: &scriptedEventsAPI{events: []cursor_api_sdk.StreamEvent{
		{Text: "secret plan", Thinking: true},
		{Text: "visible only"},
		{TurnEnded: true},
	}}}}
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
	msg := out["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "visible only" {
		t.Fatalf("content=%#v", msg["content"])
	}
	if strings.Contains(string(raw), "secret plan") {
		t.Fatalf("thinking leaked: %s", raw)
	}
}

func TestChatHTTPNonStreamToolCallsContentNull(t *testing.T) {
	h := &Handler{Server: &Server{Pool: &recPool{}, API: &scriptedEventsAPI{events: []cursor_api_sdk.StreamEvent{
		{Text: "planning", Thinking: true},
		{ToolCall: &cursor_api_sdk.PendingExec{
			ToolCallID:  "call_lookup",
			ToolName:    "lookup",
			DecodedArgs: `{"q":"x"}`,
		}},
	}}}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	body, err := json.Marshal(map[string]any{
		"model":           "composer-2.5",
		"conversation_id": "conv-tools-1",
		"messages": []map[string]string{
			{"role": "user", "content": "lookup x"},
		},
		"tools": []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name": "lookup",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"q": map[string]any{"type": "string"},
					},
				},
			},
		}},
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
	choice := out["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("finish_reason=%#v", choice["finish_reason"])
	}
	msg := choice["message"].(map[string]any)
	if msg["content"] != nil {
		t.Fatalf("expected content null, got %#v", msg["content"])
	}
	calls, _ := msg["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("tool_calls=%#v", msg["tool_calls"])
	}
	tc := calls[0].(map[string]any)
	if tc["id"] != "call_lookup" {
		t.Fatalf("tool call id=%#v", tc["id"])
	}
	// Bridge must be parked for sticky resume.
	key := cursor_api_sdk.DeriveBridgeKeyWithIdentity("composer-2.5", nil, cursor_api_sdk.ConversationIdentity{ConversationID: "conv-tools-1"})
	if br := h.Server.bridges().take(key); br == nil || br.RC == nil {
		t.Fatalf("expected parked bridge for key %q", key)
	}
}

func TestBridgeMissStreamFallsBackToResumeAction(t *testing.T) {
	api := &captureResumeAPI{text: "stream resumed"}
	h := &Handler{Server: &Server{Pool: &recPool{}, API: api}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	payload := map[string]any{
		"model":           "composer-2.5",
		"stream":          true,
		"conversation_id": "conv-sticky-stream",
		"messages": []map[string]any{
			{"role": "user", "content": "lookup x"},
			{"role": "assistant", "content": nil, "tool_calls": []map[string]any{{
				"id":   "call_1",
				"type": "function",
				"function": map[string]any{
					"name":      "lookup",
					"arguments": `{"q":"x"}`,
				},
			}}},
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
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.StatusCode, raw)
	}
	if !api.sawResume {
		t.Fatal("expected StartRun with ResumeAction after stream bridge miss")
	}
	s := string(raw)
	if !strings.Contains(s, "stream resumed") {
		t.Fatalf("body=%s", s)
	}
	if !strings.Contains(s, "data: [DONE]") {
		t.Fatalf("missing DONE: %s", s)
	}
}

func TestBridgeMissStreamToolOnlyWithoutHistoryStill400(t *testing.T) {
	h := &Handler{Server: &Server{Pool: &recPool{}, API: &recAPI{}}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	payload := map[string]any{
		"model":           "composer-2.5",
		"stream":          true,
		"conversation_id": "conv-sticky-stream",
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
	if !strings.Contains(string(raw), "No user message") {
		t.Fatalf("expected missing-user error, got %s", raw)
	}
}

func TestStreamFromRunToolCallsEmitsRoleAndParks(t *testing.T) {
	ch := make(chan cursor_api_sdk.StreamEvent, 2)
	ch <- cursor_api_sdk.StreamEvent{Text: "thinking", Thinking: true}
	ch <- cursor_api_sdk.StreamEvent{ToolCall: &cursor_api_sdk.PendingExec{
		ToolCallID:  "call_1",
		ToolName:    "lookup",
		DecodedArgs: `{}`,
	}}
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
	bridgeKey := "bridge-tool-1"
	if err := streamFromRun(dw, rec, commitSSE, &committed, "chatcmpl-1", 1, "m", bridgeKey, rc, h, "", "", envelopeChat, false); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if strings.Contains(body, "thinking") {
		t.Fatalf("thinking leaked: %s", body)
	}
	if !strings.Contains(body, `"role":"assistant"`) {
		t.Fatalf("missing role: %s", body)
	}
	if !strings.Contains(body, `"finish_reason":"tool_calls"`) {
		t.Fatalf("missing tool_calls finish: %s", body)
	}
	if !strings.Contains(body, `"id":"call_1"`) {
		t.Fatalf("missing tool call id: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("missing DONE: %s", body)
	}
	br := h.Server.bridges().take(bridgeKey)
	if br == nil || br.RC != rc {
		t.Fatalf("expected parked bridge, got %#v", br)
	}
}

func TestStreamFromRunIncompleteCompletionsEnvelope(t *testing.T) {
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
	if err := streamFromRun(dw, rec, commitSSE, &committed, "cmpl-1", 1, "m", "bridge", rc, h, "", "", envelopeCompletions, false); err != nil {
		t.Fatalf("expected nil after committed incomplete close, got %v", err)
	}
	body := rec.Body.String()
	if strings.Contains(body, "chat.completion") {
		t.Fatalf("must use text_completion envelope: %s", body)
	}
	if !strings.Contains(body, `"object":"text_completion"`) {
		t.Fatalf("missing text_completion: %s", body)
	}
	if !strings.Contains(body, `"text":"partial"`) {
		t.Fatalf("missing text: %s", body)
	}
	if !strings.Contains(body, `"finish_reason":"stop"`) {
		t.Fatalf("missing finish_reason: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("missing DONE: %s", body)
	}
	if strings.Contains(body, `"role":"assistant"`) {
		t.Fatalf("completions envelope must not emit chat role: %s", body)
	}
}

func TestWriteSSEFinishAndUsageEnvelopes(t *testing.T) {
	u := cursor_api_sdk.Usage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8}

	t.Run("chat", func(t *testing.T) {
		rec := httptest.NewRecorder()
		if err := writeSSEFinish(rec, "chatcmpl-1", 1, "m", "stop", envelopeChat); err != nil {
			t.Fatal(err)
		}
		if err := writeSSEUsage(rec, "chatcmpl-1", 1, "m", u, envelopeChat); err != nil {
			t.Fatal(err)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `"object":"chat.completion.chunk"`) {
			t.Fatalf("missing chat chunk: %s", body)
		}
		if !strings.Contains(body, `"finish_reason":"stop"`) {
			t.Fatalf("missing finish: %s", body)
		}
		if !strings.Contains(body, `"prompt_tokens":3`) || !strings.Contains(body, `"completion_tokens":5`) {
			t.Fatalf("missing usage: %s", body)
		}
	})

	t.Run("completions", func(t *testing.T) {
		rec := httptest.NewRecorder()
		if err := writeSSEFinish(rec, "cmpl-1", 1, "m", "stop", envelopeCompletions); err != nil {
			t.Fatal(err)
		}
		if err := writeSSEUsage(rec, "cmpl-1", 1, "m", u, envelopeCompletions); err != nil {
			t.Fatal(err)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `"object":"text_completion"`) {
			t.Fatalf("missing text_completion: %s", body)
		}
		if strings.Contains(body, "chat.completion") {
			t.Fatalf("must not use chat object: %s", body)
		}
		if !strings.Contains(body, `"finish_reason":"stop"`) {
			t.Fatalf("missing finish: %s", body)
		}
		if !strings.Contains(body, `"total_tokens":8`) {
			t.Fatalf("missing usage: %s", body)
		}
	})
}

func TestConsumeRunIncomplete(t *testing.T) {
	ch := make(chan cursor_api_sdk.StreamEvent, 1)
	ch <- cursor_api_sdk.StreamEvent{Text: "partial"}
	close(ch)
	rc := &cursor_api_sdk.RunControl{Events: ch}

	text, calls, err := consumeRun(rc, 0)
	if !errors.Is(err, cursor_api_sdk.ErrIncompleteRun) {
		t.Fatalf("err=%v, want ErrIncompleteRun", err)
	}
	if text != "partial" {
		t.Fatalf("text=%q", text)
	}
	if len(calls) != 0 {
		t.Fatalf("calls=%#v", calls)
	}
}

func TestStreamFromRunIncludeUsageGate(t *testing.T) {
	run := func(includeUsage bool) string {
		t.Helper()
		ch := make(chan cursor_api_sdk.StreamEvent, 2)
		ch <- cursor_api_sdk.StreamEvent{Text: "hello"}
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
		if err := streamFromRun(dw, rec, commitSSE, &committed, "chatcmpl-1", 1, "m", "bridge", rc, h, "", "", envelopeChat, includeUsage); err != nil {
			t.Fatal(err)
		}
		return rec.Body.String()
	}

	without := run(false)
	if strings.Contains(without, `"usage"`) {
		t.Fatalf("include_usage=false leaked usage: %s", without)
	}
	if !strings.Contains(without, "data: [DONE]") {
		t.Fatalf("missing DONE: %s", without)
	}

	with := run(true)
	if !strings.Contains(with, `"usage"`) {
		t.Fatalf("include_usage=true missing usage: %s", with)
	}
	if !strings.Contains(with, `"prompt_tokens"`) {
		t.Fatalf("include_usage=true missing prompt_tokens: %s", with)
	}
}
