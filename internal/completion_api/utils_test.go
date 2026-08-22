package completion_api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseBoolish(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    bool
		wantErr bool
	}{
		{name: "empty", raw: "", want: false},
		{name: "null", raw: "null", want: false},
		{name: "true", raw: "true", want: true},
		{name: "false", raw: "false", want: false},
		{name: "string true", raw: `"true"`, want: true},
		{name: "string 1", raw: `"1"`, want: true},
		{name: "string yes", raw: `"yes"`, want: true},
		{name: "string no", raw: `"no"`, want: false},
		{name: "number 1", raw: "1", want: true},
		{name: "number 0", raw: "0", want: false},
		{name: "object", raw: `{}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got bool
			err := parseBoolish(json.RawMessage(tc.raw), &got)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestValidateN(t *testing.T) {
	if err := validateN(nil); err != nil {
		t.Fatalf("nil: %v", err)
	}
	one := 1
	if err := validateN(&one); err != nil {
		t.Fatalf("1: %v", err)
	}
	two := 2
	if err := validateN(&two); err == nil || !strings.Contains(err.Error(), "n must be 1") {
		t.Fatalf("2: %v", err)
	}
}

func TestIncludeStreamUsage(t *testing.T) {
	if includeStreamUsage(nil) {
		t.Fatal("nil opts should be false")
	}
	if includeStreamUsage(&StreamOptions{}) {
		t.Fatal("zero opts should be false")
	}
	if !includeStreamUsage(&StreamOptions{IncludeUsage: true}) {
		t.Fatal("IncludeUsage true should gate on")
	}
}

func TestParseCompletionsPrompt(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{name: "missing", raw: "", wantErr: "prompt is required"},
		{name: "null", raw: "null", wantErr: "prompt is required"},
		{name: "empty string", raw: `""`, wantErr: "prompt is required"},
		{name: "whitespace string", raw: `"   "`, wantErr: "prompt is required"},
		{name: "string", raw: `"hello"`, want: "hello"},
		{name: "array", raw: `["a","b"]`, want: "a\nb"},
		{name: "empty array", raw: `[]`, wantErr: "prompt is required"},
		{name: "blank array", raw: `[" ","\t"]`, wantErr: "prompt is required"},
		{name: "object", raw: `{"x":1}`, wantErr: "prompt must be a string or array of strings"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCompletionsPrompt(json.RawMessage(tc.raw))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err=%v want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestCompletionsRequestBoolishAndN(t *testing.T) {
	var req CompletionsRequest
	if err := json.Unmarshal([]byte(`{"model":"m","prompt":"hi","stream":"true","n":1}`), &req); err != nil {
		t.Fatal(err)
	}
	if !req.Stream {
		t.Fatal("stream string true should parse true")
	}
	if err := validateN(req.N); err != nil {
		t.Fatal(err)
	}

	var bad CompletionsRequest
	if err := json.Unmarshal([]byte(`{"model":"m","prompt":"hi","n":2}`), &bad); err != nil {
		t.Fatal(err)
	}
	if err := validateN(bad.N); err == nil {
		t.Fatal("expected n=2 rejection")
	}
}

func TestCompletionsHTTPRejectsBadPromptAndN(t *testing.T) {
	h := &Handler{Server: &Server{Pool: &recPool{}, API: &scriptedTextAPI{text: "unused"}}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	post := func(payload map[string]any) (int, string) {
		t.Helper()
		body, err := json.Marshal(payload)
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
		return res.StatusCode, string(raw)
	}

	status, body := post(map[string]any{"model": "composer-2.5"})
	if status != http.StatusBadRequest || !strings.Contains(body, "prompt is required") {
		t.Fatalf("missing prompt status=%d body=%s", status, body)
	}

	status, body = post(map[string]any{"model": "composer-2.5", "prompt": []string{"a", "b"}, "n": 2})
	if status != http.StatusBadRequest || !strings.Contains(body, "n must be 1") {
		t.Fatalf("bad n status=%d body=%s", status, body)
	}

	status, body = post(map[string]any{"model": "composer-2.5", "prompt": []string{"hello", "world"}})
	if status != http.StatusOK {
		t.Fatalf("array prompt status=%d body=%s", status, body)
	}
	if !strings.Contains(body, `"object":"text_completion"`) {
		t.Fatalf("want text_completion: %s", body)
	}
	if !strings.Contains(body, `"logprobs":null`) {
		t.Fatalf("want logprobs null: %s", body)
	}
}
