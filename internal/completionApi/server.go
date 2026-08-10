package completion_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"

	account_pool "github.com/CoreUnit-NET/cursed-gateway/internal/accountPool"
	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

const maxBodyBytes = 10 << 20 // 10 MiB

// Server holds shared dependencies for OpenAI handlers.
type Server struct {
	Pool    *account_pool.Pool
	API     *cursor_api_sdk.Client
	Log     *slog.Logger
	MaxBody int64

	bridgeOnce    sync.Once
	activeBridges *bridgeRegistry
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
	cands := s.Pool.PickCandidates()
	if len(cands) == 0 {
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
			s.log().Warn("upstream error; trying next", "session", acc.ID, "err", err)
			continue
		}
		return err
	}
	if last == nil {
		last = fmt.Errorf("all accounts failed")
	}
	return last
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, msg string) {
	var body errorBody
	body.Error.Message = msg
	body.Error.Type = "server_error"
	body.Error.Code = "internal_error"
	if status == http.StatusBadRequest {
		body.Error.Type = "invalid_request_error"
		body.Error.Code = "bad_request"
	}
	writeJSON(w, status, body)
}

func readJSONBody(r *http.Request, max int64, dst any) error {
	defer r.Body.Close()
	limited := io.LimitReader(r.Body, max+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if int64(len(data)) > max {
		return fmt.Errorf("request body too large")
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
