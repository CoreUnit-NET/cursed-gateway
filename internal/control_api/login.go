package control_api

import (
	"errors"
	"net/http"

	"github.com/CoreUnit-NET/cursed-gateway/internal/login_session"
)

func loginViewFrom(a login_session.Attempt) loginView {
	return loginView{
		ID:        a.ID,
		URL:       a.URL,
		State:     a.State,
		AccountID: a.AccountID,
		Error:     a.Error,
	}
}

func (h *Handler) handleListLogin(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Attempts == nil {
		h.writeError(w, r, http.StatusInternalServerError, "login attempts are not configured")
		return
	}
	list := h.Attempts.List()
	out := loginList{LoginAttempts: make([]loginView, 0, len(list))}
	for _, a := range list {
		out.LoginAttempts = append(out.LoginAttempts, loginViewFrom(a))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) handleCreateLogin(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Attempts == nil {
		h.writeError(w, r, http.StatusInternalServerError, "login attempts are not configured")
		return
	}
	attempt, err := h.Attempts.Create()
	if err != nil {
		if errors.Is(err, login_session.ErrMaxLoginAttempts) {
			h.writeError(w, r, http.StatusConflict, err.Error())
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, loginViewFrom(attempt))
}

func (h *Handler) handleGetLogin(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Attempts == nil {
		h.writeError(w, r, http.StatusInternalServerError, "login attempts are not configured")
		return
	}
	id := r.PathValue("id")
	attempt, err := h.Attempts.Get(id)
	if err != nil {
		if errors.Is(err, login_session.ErrAttemptNotFound) {
			h.writeError(w, r, http.StatusNotFound, "login attempt not found")
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, loginViewFrom(attempt))
}

func (h *Handler) handleDeleteLogin(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Attempts == nil {
		h.writeError(w, r, http.StatusInternalServerError, "login attempts are not configured")
		return
	}
	id := r.PathValue("id")
	if err := h.Attempts.Delete(id); err != nil {
		if errors.Is(err, login_session.ErrAttemptNotFound) {
			h.writeError(w, r, http.StatusNotFound, "login attempt not found")
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
