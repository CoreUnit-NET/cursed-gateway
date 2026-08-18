package control_api

import "errors"

var (
	errBodyTooLarge = errors.New("request body too large")
	errEmptyBody    = errors.New("request body is empty")
)

type errorBody struct {
	Error string `json:"error"`
}

type accountView struct {
	ID      string `json:"id"`
	Subject string `json:"subject,omitempty"`
	Tier    string `json:"tier,omitempty"`
	Expires int64  `json:"expires"`
}

type accountList struct {
	Accounts []accountView `json:"accounts"`
}

type addAccountResponse struct {
	OK    bool   `json:"ok"`
	ID    string `json:"id,omitempty"`
	Error string `json:"error,omitempty"`
}

type loginView struct {
	ID        string `json:"id"`
	URL       string `json:"url,omitempty"`
	State     string `json:"state"`
	AccountID string `json:"account_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

type loginList struct {
	Login []loginView `json:"login"`
}

type serviceState struct {
	Accounts         int `json:"accounts"`
	LoginAttempts    int `json:"login_attempts"`
	MaxLoginAttempts int `json:"max_login_attempts"`
	LoginAttemptMins int `json:"login_attempt_mins"`
	LoginKeepMins    int `json:"login_keep_mins"`
}
