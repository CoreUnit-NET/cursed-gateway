package completion_api

import (
	"net/http"
	"testing"

	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

func TestSetCursorModelHeaders(t *testing.T) {
	h := make(http.Header)
	setCursorModelHeaders(h, cursor_api_sdk.ModelSelection{
		PublicID:    "claude-haiku-4-5",
		WireModelID: "claude-4.5-haiku-thinking",
		Parameters: []cursor_api_sdk.ModelParameter{
			{ID: "thinking", Value: "true"},
		},
	})
	if got := h.Get(headerCursorModel); got != "claude-haiku-4-5" {
		t.Fatalf("model = %q", got)
	}
	if got := h.Get(headerCursorWireModel); got != "claude-4.5-haiku-thinking" {
		t.Fatalf("wire = %q", got)
	}
	if got := h.Get(headerCursorModelParams); got != "thinking=true" {
		t.Fatalf("params = %q", got)
	}
}
