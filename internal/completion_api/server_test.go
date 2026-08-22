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

func TestOpenAIErrorTypeCodeRequestTooLarge(t *testing.T) {
	typ, code := openAIErrorTypeCode(http.StatusRequestEntityTooLarge)
	if typ != "invalid_request_error" || code != "request_too_large" {
		t.Fatalf("type=%q code=%q", typ, code)
	}
}

func TestWriteJSONDoesNotEscapeHTML(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]string{"message": `a <b> & c`})
	body := rec.Body.String()
	if strings.Contains(body, `\u003c`) || strings.Contains(body, `&lt;`) {
		t.Fatalf("HTML escaped: %s", body)
	}
	if !strings.Contains(body, `a <b> & c`) {
		t.Fatalf("missing raw angle brackets: %s", body)
	}
}

func TestMarshalJSONNoEscape(t *testing.T) {
	raw, err := marshalJSONNoEscape(map[string]string{"text": `<script>`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `\u003c`) {
		t.Fatalf("escaped: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`<script>`)) {
		t.Fatalf("missing script tag: %s", raw)
	}
	if bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatalf("marshalJSONNoEscape must trim encoder newline: %q", raw)
	}
}

func TestChatCompletionsBodyTooLarge(t *testing.T) {
	h := &Handler{Server: &Server{Pool: &recPool{}, API: &recAPI{}, MaxBody: 32, Log: slog.Default()}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/ai/v1/chat/completions", "application/json", strings.NewReader(strings.Repeat("x", 64)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s, want 413", res.StatusCode, body)
	}
	var out errorBody
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if out.Error.Type != "invalid_request_error" || out.Error.Code != "request_too_large" {
		t.Fatalf("body=%s", body)
	}
}

func TestCompletionsBodyTooLarge(t *testing.T) {
	h := &Handler{Server: &Server{Pool: &recPool{}, API: &recAPI{}, MaxBody: 32, Log: slog.Default()}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL+"/ai/v1/completions", "application/json", strings.NewReader(strings.Repeat("x", 64)))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s, want 413", res.StatusCode, body)
	}
	var out errorBody
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if out.Error.Code != "request_too_large" {
		t.Fatalf("body=%s", body)
	}
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

func TestWriteUpstreamErrorBadModelName(t *testing.T) {
	srv := &Server{Log: slog.Default()}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ai/v1/chat/completions", nil)
	err := &cursor_api_sdk.APIError{
		Code:       "not_found",
		Message:    "Error",
		DebugError: "ERROR_BAD_MODEL_NAME",
		Detail:     `Model name is not valid: "x"`,
		ModelID:    "x",
		Err:        cursor_api_sdk.ErrBadModelName,
	}
	srv.writeUpstreamError(rec, req, err)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body json: %v", err)
	}
	if body.Error.Type != "invalid_request_error" || body.Error.Code != "bad_request" {
		t.Fatalf("body = %#v", body)
	}
	if !strings.Contains(body.Error.Message, "ERROR_BAD_MODEL_NAME") {
		t.Fatalf("message = %q", body.Error.Message)
	}
}

func TestWithAccessDoesNotFailoverOnBadModelName(t *testing.T) {
	pool := &multiPool{ids: []string{"s1", "s2"}}
	srv := &Server{Pool: pool, Log: slog.Default()}
	calls := 0
	err := srv.withAccess(context.Background(), func(access string) error {
		calls++
		return &cursor_api_sdk.APIError{
			Code:       "not_found",
			DebugError: "ERROR_BAD_MODEL_NAME",
			Err:        cursor_api_sdk.ErrBadModelName,
		}
	})
	if !errors.Is(err, cursor_api_sdk.ErrBadModelName) {
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

type startErrAPI struct {
	recAPI
	err    error
	starts int
}

func (a *startErrAPI) StartRun(ctx context.Context, accessToken string, payload *cursor_api_sdk.RunPayload, bridgeTools bool) (*cursor_api_sdk.RunControl, error) {
	a.starts++
	return nil, a.err
}

func TestChatCompletionsBadModelNameReturns400(t *testing.T) {
	pool := &multiPool{ids: []string{"s1", "s2"}}
	api := &startErrAPI{err: &cursor_api_sdk.APIError{
		Code:       "not_found",
		Message:    "Error",
		DebugError: "ERROR_BAD_MODEL_NAME",
		Detail:     `Model name is not valid: "x"`,
		Err:        cursor_api_sdk.ErrBadModelName,
	}}
	h := &Handler{Server: &Server{Pool: pool, API: api, Log: slog.Default()}}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	payload := map[string]any{
		"model": "x",
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(srv.URL+"/ai/v1/chat/completions", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", res.StatusCode, body)
	}
	var out errorBody
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("json: %v body=%s", err, body)
	}
	if out.Error.Type != "invalid_request_error" || out.Error.Code != "bad_request" {
		t.Fatalf("body=%s", body)
	}
	if !strings.Contains(out.Error.Message, "ERROR_BAD_MODEL_NAME") {
		t.Fatalf("message=%q", out.Error.Message)
	}
	if api.starts != 1 {
		t.Fatalf("StartRun calls=%d, want 1", api.starts)
	}
	if len(pool.ensures) != 1 {
		t.Fatalf("ensures=%#v, want one account", pool.ensures)
	}
}
