package completion_api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/CoreUnit-NET/cursed-gateway/internal/account_pool"
	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

const maxBodyBytes = 10 << 20 // 10 MiB

// AccountPool is the account rotation surface Server needs.
// *account_pool.Pool satisfies this interface.
type AccountPool interface {
	PickCandidates() []*cursor_account_sdk.Account
	EnsureAccess(ctx context.Context, id string) (*cursor_account_sdk.Account, error)
	MarkUsed(id string)
	MarkRateLimited(id string)
}

// UpstreamAPI is the Cursor upstream surface Server needs.
// *cursor_api_sdk.Client satisfies this interface.
type UpstreamAPI interface {
	CachedModels() []cursor_api_sdk.Model
	ListModels(ctx context.Context, accessToken string) ([]cursor_api_sdk.Model, error)
	ResolveModelSelection(ctx context.Context, accessToken, modelID string) (cursor_api_sdk.ModelSelection, error)
	StartRun(ctx context.Context, accessToken string, payload *cursor_api_sdk.RunPayload, bridgeTools bool) (*cursor_api_sdk.RunControl, error)
}

// Server holds shared dependencies for OpenAI handlers.
type Server struct {
	Pool    AccountPool
	API     UpstreamAPI
	Log     *slog.Logger
	MaxBody int64

	bridgeOnce     sync.Once
	activeBridges  *bridgeRegistry
	checkpointOnce sync.Once
	checkpoints    *cursor_api_sdk.CheckpointStore
}

// NewServer wires a concrete pool and API client for handlers / CLI reuse.
func NewServer(pool *account_pool.Pool, api *cursor_api_sdk.Client, log *slog.Logger) *Server {
	return &Server{Pool: pool, API: api, Log: log}
}

func (s *Server) log() *slog.Logger {
	if s != nil && s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Server) maxBody() int64 {
	if s != nil && s.MaxBody > 0 {
		return s.MaxBody
	}
	return maxBodyBytes
}

func (s *Server) withAccess(ctx context.Context, fn func(access string) error) error {
	if s == nil || s.Pool == nil {
		return fmt.Errorf("account pool is not configured")
	}
	cands := s.Pool.PickCandidates()
	if len(cands) == 0 {
		s.log().Warn("no sessions available", "hint", "login or import")
		return fmt.Errorf("no sessions in auth store; run login or import first")
	}
	var last error
	for _, acc := range cands {
		ready, err := s.Pool.EnsureAccess(ctx, acc.ID)
		if err != nil {
			last = err
			s.log().Warn("ensure access failed", "session", acc.ID, "err", err)
			continue
		}
		err = fn(ready.Access)
		if err == nil {
			s.Pool.MarkUsed(acc.ID)
			return nil
		}
		last = err
		// Missing-blob is a bad payload (inlined Structure bytes). Rotating
		// accounts retries the same broken request and only burns sessions.
		if cursor_api_sdk.IsMissingBlob(err) {
			s.log().Error("missing blob; not rotating accounts", "session", acc.ID, "err", err)
			return err
		}
		if errors.Is(err, cursor_api_sdk.ErrBadModelName) {
			return err
		}
		if errors.Is(err, cursor_api_sdk.ErrRateLimited) {
			s.Pool.MarkRateLimited(acc.ID)
			s.log().Warn("rate limited; cooling account", "session", acc.ID)
			continue
		}
		if errors.Is(err, cursor_api_sdk.ErrUnauthorized) {
			s.log().Warn("unauthorized account; trying next", "session", acc.ID)
			continue
		}
		// Pre-stream / init style errors: try next account.
		var apiErr *cursor_api_sdk.APIError
		if errors.As(err, &apiErr) {
			s.log().Warn("upstream error; trying next",
				"session", acc.ID,
				"err", err,
				"code", apiErr.Code,
				"model", apiErr.ModelID,
			)
			continue
		}
		s.log().Error("request aborted", "session", acc.ID, "err", err)
		return err
	}
	if last == nil {
		last = fmt.Errorf("all accounts failed")
	}
	s.log().Error("all account candidates failed", "candidates", len(cands), "err", last)
	return last
}

// ListModels returns the cached catalog when fresh; otherwise fetches via the account pool.
func (s *Server) ListModels(ctx context.Context) ([]cursor_api_sdk.Model, error) {
	if s == nil || s.API == nil {
		return nil, fmt.Errorf("upstream API is not configured")
	}
	if cached := s.API.CachedModels(); len(cached) > 0 {
		return cached, nil
	}
	s.log().Debug("models cache miss; fetching via account pool")
	var models []cursor_api_sdk.Model
	err := s.withAccess(ctx, func(access string) error {
		m, err := s.API.ListModels(ctx, access)
		if err != nil {
			return err
		}
		models = m
		return nil
	})
	if err == nil {
		s.log().Debug("models catalog ready", "count", len(models))
	}
	return models, err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func (s *Server) writeAPIError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	attrs := []any{"status", status, "err", msg}
	if r != nil {
		attrs = append(attrs, "method", r.Method, "path", r.URL.Path)
	}
	switch {
	case status >= 500:
		s.log().Error("api error", attrs...)
	case status == http.StatusTooManyRequests || status == http.StatusConflict:
		s.log().Warn("api error", attrs...)
	default:
		s.log().Info("api error", attrs...)
	}
	var body errorBody
	body.Error.Message = msg
	body.Error.Type, body.Error.Code = openAIErrorTypeCode(status)
	writeJSON(w, status, body)
}

// openAIErrorTypeCode maps HTTP status to OpenAI-ish error.type / error.code.
func openAIErrorTypeCode(status int) (typ, code string) {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error", "bad_request"
	case http.StatusUnauthorized:
		return "invalid_request_error", "unauthorized"
	case http.StatusNotFound:
		return "invalid_request_error", "not_found"
	case http.StatusMethodNotAllowed:
		return "invalid_request_error", "method_not_allowed"
	case http.StatusRequestEntityTooLarge:
		return "invalid_request_error", "request_too_large"
	case http.StatusTooManyRequests:
		return "rate_limit_error", "rate_limit_exceeded"
	case http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusServiceUnavailable:
		return "api_error", "upstream_error"
	default:
		if status >= 500 {
			return "server_error", "internal_error"
		}
		return "invalid_request_error", "bad_request"
	}
}

func (s *Server) writeUpstreamError(w http.ResponseWriter, r *http.Request, err error) {
	msg := "upstream error"
	if err != nil {
		msg = err.Error()
	}
	status := http.StatusBadGateway
	if errors.Is(err, cursor_api_sdk.ErrBadModelName) {
		status = http.StatusBadRequest
	}
	s.writeAPIError(w, r, status, msg)
}

// ErrBodyTooLarge is returned by readJSONBody when the request exceeds max bytes.
var ErrBodyTooLarge = errors.New("request body too large")

func readJSONBody(r *http.Request, max int64, dst any) error {
	defer r.Body.Close()
	limited := io.LimitReader(r.Body, max+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if int64(len(data)) > max {
		return ErrBodyTooLarge
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func writeJSONBodyError(s *Server, w http.ResponseWriter, r *http.Request, err error) {
	if s == nil {
		return
	}
	status := http.StatusBadRequest
	if errors.Is(err, ErrBodyTooLarge) {
		status = http.StatusRequestEntityTooLarge
	}
	msg := "invalid request"
	if err != nil {
		msg = err.Error()
	}
	s.writeAPIError(w, r, status, msg)
}

// marshalJSONNoEscape encodes v as JSON without HTML-escaping <>&.
func marshalJSONNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buf.Bytes()), nil
}

func writeJSONValue(w io.Writer, v any) error {
	b, err := marshalJSONNoEscape(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
