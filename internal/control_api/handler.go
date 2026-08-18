package control_api

/*
Package control_api serves the Control API under /api.

REST resources: service state, accounts, and login attempts.
AI routes live under /ai (completion_api). There is no whoami.
*/

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/CoreUnit-NET/cursed-gateway/internal/login_session"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Handler serves Control API routes.
type Handler struct {
	Store    *login_session.Store
	Attempts *login_session.LoginAttempts
	Log      *slog.Logger

	MaxLoginAttempts int
	LoginAttemptMins int
	LoginKeepMins    int
}

// Mount registers /api routes on mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api", h.handleService)
	mux.HandleFunc("GET /api/accounts", h.handleListAccounts)
	mux.HandleFunc("POST /api/accounts", h.handleCreateAccount)
	mux.HandleFunc("GET /api/accounts/{id}", h.handleGetAccount)
	mux.HandleFunc("DELETE /api/accounts/{id}", h.handleDeleteAccount)
	mux.HandleFunc("GET /api/login", h.handleListLogin)
	mux.HandleFunc("POST /api/login", h.handleCreateLogin)
	mux.HandleFunc("GET /api/login/{id}", h.handleGetLogin)
	mux.HandleFunc("DELETE /api/login/{id}", h.handleDeleteLogin)
}

func (h *Handler) log() *slog.Logger {
	if h != nil && h.Log != nil {
		return h.Log
	}
	return slog.Default()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	attrs := []any{"status", status, "err", msg}
	if r != nil {
		attrs = append(attrs, "method", r.Method, "path", r.URL.Path)
	}
	h.log().Warn("control api error", attrs...)
	writeJSON(w, status, errorBody{Error: msg})
}

func readJSONBody(r *http.Request, max int64, dst any) error {
	defer r.Body.Close()
	limited := io.LimitReader(r.Body, max+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if int64(len(data)) > max {
		return errBodyTooLarge
	}
	if len(data) == 0 {
		return errEmptyBody
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return err
	}
	return nil
}
