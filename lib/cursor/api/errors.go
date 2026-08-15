package cursor_api_sdk

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrRateLimited      = errors.New("cursor rate limited")
	ErrUnauthorized     = errors.New("cursor unauthorized")
	ErrUpstream         = errors.New("cursor upstream error")
	ErrIncompleteRun    = errors.New("cursor run ended without turn_ended")
	ErrModelUnavailable = errors.New("cursor model unavailable for agent")
	ErrBadModelName     = errors.New("cursor bad model name")
	// ErrMissingBlob is a client payload bug (Structure bytes were not blob ids).
	// Account failover must not rotate — the same payload will fail again.
	ErrMissingBlob = errors.New("cursor missing blob")
)

// IsMissingBlob reports whether err looks like Cursor "Blob not found"
// (inlined Structure bytes treated as sha256 ids).
func IsMissingBlob(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrMissingBlob) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "blob not found") ||
		strings.Contains(msg, "missing blob")
}

// APIError carries HTTP status and optional Connect error details.
type APIError struct {
	Status     int
	Code       string
	Message    string
	ModelID    string
	DebugError string // e.g. ERROR_BAD_MODEL_NAME from aiserver.v1.ErrorDetails
	Title      string
	Detail     string
	Err        error
}

func (e *APIError) Error() string {
	if e == nil {
		return "cursor api error"
	}
	if errors.Is(e.Err, ErrBadModelName) {
		label := e.ModelID
		if label == "" {
			label = "unknown"
		}
		detail := strings.TrimSpace(e.Detail)
		if detail == "" {
			detail = strings.TrimSpace(e.Message)
		}
		if detail == "" {
			detail = "model name is not valid"
		}
		if e.DebugError != "" {
			return fmt.Sprintf("cursor rejected model %q: %s (%s)", label, detail, e.DebugError)
		}
		return fmt.Sprintf("cursor rejected model %q: %s", label, detail)
	}
	if errors.Is(e.Err, ErrModelUnavailable) {
		label := e.ModelID
		if label == "" {
			label = "this model"
		}
		if e.Detail != "" {
			return fmt.Sprintf("%q is listed by Cursor but is not available for agent requests on this account (%s)", label, e.Detail)
		}
		return fmt.Sprintf("%q is listed by Cursor but is not available for agent requests on this account (connect %s: %s)", label, e.Code, e.Message)
	}
	if e.Code != "" {
		if e.Detail != "" {
			return fmt.Sprintf("cursor api: HTTP %d %s: %s", e.Status, e.Code, e.Detail)
		}
		return fmt.Sprintf("cursor api: HTTP %d %s: %s", e.Status, e.Code, e.Message)
	}
	if e.Message != "" {
		return fmt.Sprintf("cursor api: HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("cursor api: HTTP %d", e.Status)
}

func (e *APIError) Unwrap() error { return e.Err }

func classifyHTTP(status int, body string) error {
	switch {
	case status == 429:
		return &APIError{Status: status, Message: body, Err: ErrRateLimited}
	case status == 401 || status == 403:
		return &APIError{Status: status, Message: body, Err: ErrUnauthorized}
	case status >= 500:
		err := ErrUpstream
		if IsMissingBlob(errors.New(body)) {
			err = ErrMissingBlob
		}
		return &APIError{Status: status, Message: body, Err: err}
	default:
		return &APIError{Status: status, Message: body, Err: ErrUpstream}
	}
}

type connectDebugInfo struct {
	Error  string
	Title  string
	Detail string
}

func classifyConnectCode(code, message string, dbg connectDebugInfo) error {
	err := &APIError{
		Code:       code,
		Message:    message,
		DebugError: dbg.Error,
		Title:      dbg.Title,
		Detail:     dbg.Detail,
	}
	switch code {
	case "resource_exhausted", "8":
		err.Err = ErrRateLimited
	case "unauthenticated", "16", "permission_denied", "7":
		err.Err = ErrUnauthorized
	case "not_found", "5":
		if dbg.Error == "ERROR_BAD_MODEL_NAME" {
			err.Err = ErrBadModelName
		} else {
			err.Err = ErrModelUnavailable
		}
	default:
		err.Err = ErrUpstream
	}
	if IsMissingBlob(errors.New(message)) || IsMissingBlob(errors.New(dbg.Detail)) {
		err.Err = ErrMissingBlob
	}
	return err
}

// WithModelID annotates an APIError with the requested model id when present.
func WithModelID(err error, modelID string) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr != nil {
		cp := *apiErr
		cp.ModelID = modelID
		return &cp
	}
	return err
}
