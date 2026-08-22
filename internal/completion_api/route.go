package completion_api

/*
Package completion_api serves the OpenAI-compatible HTTP surface.

AI base is /ai/v1. /v1 and unprefixed routes stay as aliases.

Routes:
  GET  /ai/v1/models
  POST /ai/v1/chat/completions
  POST /ai/v1/completions  (text_completion envelope; same Cursor run)
*/

import (
	"net/http"
)

// Handler serves OpenAI-compatible endpoints.
type Handler struct {
	Server *Server
}

// Mount registers AI routes on mux under /ai/v1, plus /v1 and unprefixed aliases.
func (h *Handler) Mount(mux *http.ServeMux) {
	h.mountAI(mux, "/ai/v1")
	h.mountAI(mux, "/v1")
	h.mountAI(mux, "")
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
}

func (h *Handler) mountAI(mux *http.ServeMux, prefix string) {
	mux.HandleFunc("GET "+prefix+"/models", h.handleModels)
	mux.HandleFunc("POST "+prefix+"/chat/completions", h.handleChatCompletions)
	mux.HandleFunc("POST "+prefix+"/completions", h.handleCompletions)
}
