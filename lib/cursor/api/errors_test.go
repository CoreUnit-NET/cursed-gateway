package cursor_api_sdk

import (
	"errors"
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
