package control_api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/CoreUnit-NET/cursed-gateway/internal/login_session"
	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

func accountViewFrom(a *cursor_account_sdk.Account) accountView {
	if a == nil {
		return accountView{}
	}
	return accountView{
		ID:      login_session.PublicAccountID(a),
		Subject: a.Subject,
		Tier:    a.Tier,
		Expires: a.ExpiresAt,
	}
}

func (h *Handler) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Store == nil {
		h.writeError(w, r, http.StatusInternalServerError, "account store is not configured")
		return
	}
	list := h.Store.List()
	out := accountList{Accounts: make([]accountView, 0, len(list))}
	for _, a := range list {
		out.Accounts = append(out.Accounts, accountViewFrom(a))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Store == nil {
		h.writeError(w, r, http.StatusInternalServerError, "account store is not configured")
		return
	}
	id := r.PathValue("id")
	acc, err := h.Store.Find(id)
	if err != nil {
		if errors.Is(err, login_session.ErrNotFound) {
			h.writeError(w, r, http.StatusNotFound, "account not found")
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, accountViewFrom(acc))
}

func (h *Handler) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Store == nil {
		h.writeError(w, r, http.StatusInternalServerError, "account store is not configured")
		return
	}
	id := r.PathValue("id")
	_, err := h.Store.RemoveMatch(id)
	if err != nil {
		if errors.Is(err, login_session.ErrNotFound) {
			h.writeError(w, r, http.StatusNotFound, "account not found")
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Store == nil {
		h.writeError(w, r, http.StatusInternalServerError, "account store is not configured")
		return
	}
	var raw map[string]any
	if err := readJSONBody(r, maxBodyBytes, &raw); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errBodyTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, status, addAccountResponse{OK: false, Error: err.Error()})
		return
	}
	body, err := json.Marshal(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, addAccountResponse{OK: false, Error: err.Error()})
		return
	}
	creds, err := login_session.ParseAuthPayload(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, addAccountResponse{OK: false, Error: err.Error()})
		return
	}
	account, merged, err := h.Store.TestAndStore(r.Context(), creds)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, login_session.ErrInvalidImport) || errors.Is(err, cursor_account_sdk.ErrMissingRefreshToken) {
			status = http.StatusBadRequest
		} else if errors.Is(err, cursor_account_sdk.ErrRefreshRejected) {
			status = http.StatusUnauthorized
		}
		writeJSON(w, status, addAccountResponse{OK: false, Error: err.Error()})
		return
	}
	status := http.StatusCreated
	if merged {
		status = http.StatusOK
	}
	writeJSON(w, status, addAccountResponse{OK: true, ID: login_session.PublicAccountID(account)})
}
