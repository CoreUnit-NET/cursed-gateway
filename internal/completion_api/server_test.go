package completion_api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

type recPool struct {
	picks int
}

func (p *recPool) PickCandidates() []*cursor_account_sdk.Account {
	p.picks++
	return []*cursor_account_sdk.Account{{ID: "s1", Access: "tok"}}
}

func (p *recPool) EnsureAccess(ctx context.Context, id string) (*cursor_account_sdk.Account, error) {
	return &cursor_account_sdk.Account{ID: id, Access: "tok"}, nil
}

func (p *recPool) MarkUsed(id string) {}

func (p *recPool) MarkRateLimited(id string) {}

type recAPI struct {
	lists int
	cache []cursor_api_sdk.Model
}

func (a *recAPI) CachedModels() []cursor_api_sdk.Model {
	return a.cache
}

func (a *recAPI) ListModels(ctx context.Context, accessToken string) ([]cursor_api_sdk.Model, error) {
	a.lists++
	a.cache = []cursor_api_sdk.Model{{ID: "composer-2.5", Name: "Composer 2.5"}}
	return a.cache, nil
}

func (a *recAPI) ResolveModelSelection(ctx context.Context, accessToken, modelID string) (cursor_api_sdk.ModelSelection, error) {
	return cursor_api_sdk.LiteralModelSelection(modelID), nil
}

func (a *recAPI) StartRun(ctx context.Context, accessToken string, payload *cursor_api_sdk.RunPayload, bridgeTools bool) (*cursor_api_sdk.RunControl, error) {
	return nil, fmt.Errorf("unused")
}

func TestListModelsUsesCacheWithoutPool(t *testing.T) {
	pool := &recPool{}
	api := &recAPI{}
	srv := &Server{Pool: pool, API: api}

	first, err := srv.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].ID != "composer-2.5" {
		t.Fatalf("first = %#v", first)
	}
	if pool.picks != 1 || api.lists != 1 {
		t.Fatalf("after first: picks=%d lists=%d", pool.picks, api.lists)
	}

	second, err := srv.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].ID != "composer-2.5" {
		t.Fatalf("second = %#v", second)
	}
	if pool.picks != 1 || api.lists != 1 {
		t.Fatalf("cache hit must skip pool: picks=%d lists=%d", pool.picks, api.lists)
	}
}

type multiPool struct {
	ids         []string
	ensures     []string
	used        []string
	rateLimited []string
}

func (p *multiPool) PickCandidates() []*cursor_account_sdk.Account {
	out := make([]*cursor_account_sdk.Account, len(p.ids))
	for i, id := range p.ids {
		out[i] = &cursor_account_sdk.Account{ID: id, Access: "tok-" + id}
	}
	return out
}

func (p *multiPool) EnsureAccess(ctx context.Context, id string) (*cursor_account_sdk.Account, error) {
	p.ensures = append(p.ensures, id)
	return &cursor_account_sdk.Account{ID: id, Access: "tok-" + id}, nil
}

func (p *multiPool) MarkUsed(id string) { p.used = append(p.used, id) }

func (p *multiPool) MarkRateLimited(id string) {
	p.rateLimited = append(p.rateLimited, id)
}

func TestWithAccessDoesNotFailoverOnMissingBlob(t *testing.T) {
	pool := &multiPool{ids: []string{"s1", "s2"}}
	srv := &Server{Pool: pool, Log: slog.Default()}
	calls := 0
	err := srv.withAccess(context.Background(), func(access string) error {
		calls++
		return &cursor_api_sdk.APIError{
			Status:  502,
			Code:    "internal",
			Message: "Blob not found",
			Err:     cursor_api_sdk.ErrMissingBlob,
		}
	})
	if !cursor_api_sdk.IsMissingBlob(err) {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 {
		t.Fatalf("fn calls = %d, want 1 (no account rotation)", calls)
	}
	if len(pool.ensures) != 1 || pool.ensures[0] != "s1" {
		t.Fatalf("ensures = %#v", pool.ensures)
	}
	if len(pool.used) != 0 {
		t.Fatalf("MarkUsed should not run: %#v", pool.used)
	}
}

func TestWithAccessFailoversOnRateLimit(t *testing.T) {
	pool := &multiPool{ids: []string{"s1", "s2"}}
	srv := &Server{Pool: pool, Log: slog.Default()}
	calls := 0
	err := srv.withAccess(context.Background(), func(access string) error {
		calls++
		if access == "tok-s1" {
			return cursor_api_sdk.ErrRateLimited
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("fn calls = %d, want 2", calls)
	}
	if len(pool.rateLimited) != 1 || pool.rateLimited[0] != "s1" {
		t.Fatalf("rateLimited = %#v", pool.rateLimited)
	}
	if len(pool.used) != 1 || pool.used[0] != "s2" {
		t.Fatalf("used = %#v", pool.used)
	}
}

func TestWriteAPIErrorLogsInfoForBadRequest(t *testing.T) {
	var logBuf bytes.Buffer
	srv := &Server{
		Log: slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	msg := "invalid JSON: content must be a string or array of parts"

	srv.writeAPIError(rec, req, http.StatusBadRequest, msg)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body json: %v", err)
	}
	if body.Error.Message != msg || body.Error.Type != "invalid_request_error" || body.Error.Code != "bad_request" {
		t.Fatalf("body = %#v", body)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "level=INFO") {
		t.Fatalf("expected INFO log, got %q", logged)
	}
	if strings.Contains(logged, "level=WARN") || strings.Contains(logged, "level=ERROR") {
		t.Fatalf("400 must not warn/error, got %q", logged)
	}
	if !strings.Contains(logged, "msg=\"api error\"") {
		t.Fatalf("expected api error msg, got %q", logged)
	}
	if !strings.Contains(logged, "status=400") {
		t.Fatalf("expected status attr, got %q", logged)
	}
	if !strings.Contains(logged, "method=POST") || !strings.Contains(logged, "path=/v1/chat/completions") {
		t.Fatalf("expected method/path attrs, got %q", logged)
	}
	if !strings.Contains(logged, "invalid JSON") {
		t.Fatalf("expected err attr, got %q", logged)
	}
}

func TestWriteAPIErrorLogsErrorForBadGateway(t *testing.T) {
	var logBuf bytes.Buffer
	srv := &Server{
		Log: slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ai/v1/models", nil)
	srv.writeAPIError(rec, req, http.StatusBadGateway, "no sessions in auth store; run login or import first")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	logged := logBuf.String()
	if !strings.Contains(logged, "level=ERROR") {
		t.Fatalf("expected ERROR log, got %q", logged)
	}
	if !strings.Contains(logged, "status=502") {
		t.Fatalf("expected status attr, got %q", logged)
	}
}
