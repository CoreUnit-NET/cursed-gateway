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

func TestWriteAPIErrorLogsWarn(t *testing.T) {
	var logBuf bytes.Buffer
	srv := &Server{
		Log: slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})),
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
	if !strings.Contains(logged, "level=WARN") {
		t.Fatalf("expected WARN log, got %q", logged)
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
