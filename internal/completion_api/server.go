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
	ListModels(ctx context.Context, accessToken string) ([]cursor_api_sdk.Model, error)
	StartRun(ctx context.Context, accessToken string, payload *cursor_api_sdk.RunPayload, bridgeTools bool) (*cursor_api_sdk.RunControl, error)
}

// Server holds shared dependencies for OpenAI handlers.
type Server struct {
	Pool    AccountPool
	API     UpstreamAPI
	Log     *slog.Logger
	MaxBody int64

	bridgeOnce    sync.Once
	activeBridges *bridgeRegistry
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

// ListModels fetches Cursor models using the account pool.
func (s *Server) ListModels(ctx context.Context) ([]cursor_api_sdk.Model, error) {
	if s == nil || s.API == nil {
		return nil, fmt.Errorf("upstream API is not configured")
	}
	var models []cursor_api_sdk.Model
	err := s.withAccess(ctx, func(access string) error {
		m, err := s.API.ListModels(ctx, access)
		if err != nil {
			return err
		}
		models = m
		return nil
	})
	return models, err
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
