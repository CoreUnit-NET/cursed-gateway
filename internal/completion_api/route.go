package completion_api

/*
Package completion_api serves the OpenAI-compatible HTTP surface.

Routes:
  GET  /v1/models
  POST /v1/chat/completions
  POST /v1/completions  (thin wrapper around chat)
*/

import (
	"net/http"
)

// Handler serves OpenAI-compatible endpoints.
type Handler struct {
	Server *Server
}

// Mount registers routes on mux (paths under /v1/...).
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/models", h.handleModels)
	mux.HandleFunc("GET /models", h.handleModels)
	mux.HandleFunc("POST /v1/chat/completions", h.handleChatCompletions)
	mux.HandleFunc("POST /chat/completions", h.handleChatCompletions)
	mux.HandleFunc("POST /v1/completions", h.handleCompletions)
	mux.HandleFunc("POST /completions", h.handleCompletions)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
}
