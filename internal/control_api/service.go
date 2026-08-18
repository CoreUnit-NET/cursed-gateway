package control_api

import "net/http"

func (h *Handler) handleService(w http.ResponseWriter, r *http.Request) {
	accounts := 0
	if h != nil && h.Store != nil {
		accounts = len(h.Store.List())
	}
	loginAttempts := 0
	if h != nil && h.Attempts != nil {
		loginAttempts = len(h.Attempts.List())
	}
	maxOpen := 3
	attemptMins := 3
	keepMins := 5
	if h != nil {
		if h.MaxLoginAttempts > 0 {
			maxOpen = h.MaxLoginAttempts
		}
		if h.LoginAttemptMins > 0 {
			attemptMins = h.LoginAttemptMins
		}
		if h.LoginKeepMins > 0 {
			keepMins = h.LoginKeepMins
		}
	}
	writeJSON(w, http.StatusOK, serviceState{
		Accounts:         accounts,
		LoginAttempts:    loginAttempts,
		MaxLoginAttempts: maxOpen,
		LoginAttemptMins: attemptMins,
		LoginKeepMins:    keepMins,
	})
}
