package completion_api

import (
	"net/http"

	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

const (
	headerCursorModel       = "X-Cursor-Model"
	headerCursorWireModel   = "X-Cursor-Wire-Model"
	headerCursorModelParams = "X-Cursor-Model-Params"
)

func setCursorModelHeaders(h http.Header, sel cursor_api_sdk.ModelSelection) {
	if h == nil {
		return
	}
	if sel.PublicID != "" {
		h.Set(headerCursorModel, sel.PublicID)
	}
	if sel.WireModelID != "" {
		h.Set(headerCursorWireModel, sel.WireModelID)
	}
	if params := cursor_api_sdk.FormatModelParameters(sel.Parameters); params != "" {
		h.Set(headerCursorModelParams, params)
	}
}
