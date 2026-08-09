package cursor_api_sdk

import (
	"errors"
	"fmt"
)

var (
	ErrRateLimited   = errors.New("cursor rate limited")
	ErrUnauthorized  = errors.New("cursor unauthorized")
	ErrUpstream      = errors.New("cursor upstream error")
	ErrIncompleteRun = errors.New("cursor run ended without turn_ended")
)

// APIError carries HTTP status and optional Connect error details.
type APIError struct {
	Status  int
	Code    string
	Message string
	Err     error
}

func (e *APIError) Error() string {
	if e == nil {
		return "cursor api error"
	}
	if e.Code != "" {
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
		return &APIError{Status: status, Message: body, Err: ErrUpstream}
	default:
		return &APIError{Status: status, Message: body, Err: ErrUpstream}
	}
}

func classifyConnectCode(code, message string) error {
	switch code {
	case "resource_exhausted", "8":
		return &APIError{Code: code, Message: message, Err: ErrRateLimited}
	case "unauthenticated", "16", "permission_denied", "7":
		return &APIError{Code: code, Message: message, Err: ErrUnauthorized}
	default:
		return &APIError{Code: code, Message: message, Err: ErrUpstream}
	}
}
