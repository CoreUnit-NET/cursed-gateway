package cursor_api_sdk

import (
	"errors"
	"fmt"
	"testing"
)

func TestParseConnectEndStreamBadModelName(t *testing.T) {
	raw := []byte(`{
		"error":{
			"code":"not_found",
			"message":"Error",
			"details":[{
				"type":"aiserver.v1.ErrorDetails",
				"debug":{
					"error":"ERROR_BAD_MODEL_NAME",
					"details":{
						"title":"AI Model Not Found",
						"detail":"Model name is not valid: \"claude-haiku-4-5\"",
						"isRetryable":false
					},
					"isExpected":true
				}
			}]
		}
	}`)
	err := parseConnectEndStream(raw)
	if !errors.Is(err, ErrBadModelName) {
		t.Fatalf("err = %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatal("expected APIError")
	}
	if apiErr.DebugError != "ERROR_BAD_MODEL_NAME" {
		t.Fatalf("debug = %q", apiErr.DebugError)
	}
	if apiErr.Detail != `Model name is not valid: "claude-haiku-4-5"` {
		t.Fatalf("detail = %q", apiErr.Detail)
	}
	msg := WithModelID(err, "claude-haiku-4-5").Error()
	if !errors.Is(WithModelID(err, "claude-haiku-4-5"), ErrBadModelName) {
		t.Fatal("WithModelID lost ErrBadModelName")
	}
	if want := `cursor rejected model "claude-haiku-4-5": Model name is not valid: "claude-haiku-4-5" (ERROR_BAD_MODEL_NAME)`; msg != want {
		t.Fatalf("message = %q", msg)
	}
}

func TestParseConnectEndStreamGenericNotFound(t *testing.T) {
	raw := []byte(`{"error":{"code":"not_found","message":"missing"}}`)
	err := parseConnectEndStream(raw)
	if !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestFormatModelParameters(t *testing.T) {
	got := FormatModelParameters([]ModelParameter{
		{ID: "thinking", Value: "true"},
		{ID: "fast", Value: "false"},
	})
	if got != "thinking=true,fast=false" {
		t.Fatalf("got %q", got)
	}
}

func TestIsMissingBlob(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "sentinel", err: ErrMissingBlob, want: true},
		{name: "wrapped", err: fmt.Errorf("run failed: %w", ErrMissingBlob), want: true},
		{name: "blob not found", err: fmt.Errorf("Connect error internal: Blob not found"), want: true},
		{name: "missing blob", err: fmt.Errorf("missing blob for id abc"), want: true},
		{name: "unrelated", err: ErrRateLimited, want: false},
		{name: "api upstream blob", err: &APIError{Status: 500, Message: "Blob not found", Err: ErrMissingBlob}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsMissingBlob(tc.err); got != tc.want {
				t.Fatalf("IsMissingBlob(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestClassifyHTTPMissingBlob(t *testing.T) {
	err := classifyHTTP(502, "Connect error internal: Blob not found")
	if !errors.Is(err, ErrMissingBlob) {
		t.Fatalf("err = %v", err)
	}
	if !IsMissingBlob(err) {
		t.Fatal("expected IsMissingBlob")
	}
}
