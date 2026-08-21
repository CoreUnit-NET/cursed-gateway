package control_api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CoreUnit-NET/cursed-gateway/internal/login_session"
	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

func TestControlAPIAccountsAndService(t *testing.T) {
	refresh := fakeJWT(t, "user_api", time.Now().Add(24*time.Hour))
	access := fakeJWT(t, "user_api", time.Now().Add(time.Hour))
	refreshSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken":  access,
			"refreshToken": refresh,
		})
	}))
	t.Cleanup(refreshSrv.Close)

	store, err := login_session.NewStore(filepath.Join(t.TempDir(), "data.json"), &cursor_account_sdk.Client{
		HTTP: refreshSrv.Client(),
		Endpoints: cursor_account_sdk.Endpoints{
			RefreshURL: refreshSrv.URL,
		},
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	h := &Handler{
		Store:            store,
		Attempts:         &login_session.LoginAttempts{Store: store},
		MaxLoginAttempts: 4,
		LoginAttemptMins: 2,
		LoginKeepMins:    7,
	}
	srv := httptest.NewServer(testMux(h))
	t.Cleanup(srv.Close)

	res, body := doJSON(t, srv, http.MethodGet, "/api/status", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/status status=%d body=%s", res.StatusCode, body)
	}
	var state serviceState
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatalf("service json: %v", err)
	}
	if state.Accounts != 0 || state.LoginAttempts != 0 || state.MaxLoginAttempts != 4 || state.LoginAttemptMins != 2 || state.LoginKeepMins != 7 {
		t.Fatalf("service = %+v", state)
	}

	res, body = doJSON(t, srv, http.MethodPost, "/api/accounts", map[string]string{"refreshToken": refresh})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/accounts status=%d body=%s", res.StatusCode, body)
	}
	var created addAccountResponse
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("create json: %v", err)
	}
	if !created.OK || created.ID != "user_api" {
		t.Fatalf("created = %+v", created)
	}
	if strings.Contains(string(body), access) || strings.Contains(string(body), refresh) {
		t.Fatalf("tokens leaked in create response: %s", body)
	}

	res, body = doJSON(t, srv, http.MethodPost, "/api/accounts", map[string]string{"refreshToken": refresh})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST merge status=%d body=%s", res.StatusCode, body)
	}

	res, body = doJSON(t, srv, http.MethodGet, "/api/accounts", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/accounts status=%d body=%s", res.StatusCode, body)
	}
	if strings.Contains(string(body), access) || strings.Contains(string(body), refresh) {
		t.Fatalf("tokens leaked in list: %s", body)
	}
	var list accountList
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("list json: %v", err)
	}
	if len(list.Accounts) != 1 || list.Accounts[0].ID != "user_api" || list.Accounts[0].Subject != "user_api" {
		t.Fatalf("list = %+v", list)
	}

	res, body = doJSON(t, srv, http.MethodGet, "/api/accounts/user_api", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET account status=%d body=%s", res.StatusCode, body)
	}
	var one accountView
	if err := json.Unmarshal(body, &one); err != nil {
		t.Fatalf("get json: %v", err)
	}
	if one.ID != "user_api" || one.Tier == "" {
		t.Fatalf("account = %+v", one)
	}

	res, _ = doJSON(t, srv, http.MethodDelete, "/api/accounts/user_api", nil)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status=%d", res.StatusCode)
	}
	res, _ = doJSON(t, srv, http.MethodGet, "/api/accounts/user_api", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET deleted status=%d", res.StatusCode)
	}
}

func TestControlAPILoginAttempts(t *testing.T) {
	store, err := login_session.NewStore(filepath.Join(t.TempDir(), "data.json"), &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	access := fakeJWT(t, "user_pkce", time.Now().Add(time.Hour))
	refresh := fakeJWT(t, "user_pkce", time.Now().Add(24*time.Hour))
	block := make(chan struct{})
	attempts := &login_session.LoginAttempts{
		Store:          store,
		MaxOpen:        1,
		AttemptTimeout: time.Minute,
		Keep:           time.Minute,
		Poll: func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
			select {
			case <-ctx.Done():
				return cursor_account_sdk.Credentials{}, ctx.Err()
			case <-block:
				return cursor_account_sdk.Credentials{
					Access:    access,
					Refresh:   refresh,
					ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
				}, nil
			}
		},
	}
	t.Cleanup(attempts.Stop)

	h := &Handler{Store: store, Attempts: attempts, MaxLoginAttempts: 1}
	srv := httptest.NewServer(testMux(h))
	t.Cleanup(srv.Close)

	res, body := doJSON(t, srv, http.MethodPost, "/api/login-attempts", nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/login-attempts status=%d body=%s", res.StatusCode, body)
	}
	var created loginView
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("create json: %v", err)
	}
	if created.ID == "" || created.URL == "" || created.State != login_session.AttemptPending {
		t.Fatalf("created = %+v", created)
	}

	res, body = doJSON(t, srv, http.MethodPost, "/api/login-attempts", nil)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("second POST status=%d body=%s, want 409", res.StatusCode, body)
	}

	close(block)
	deadline := time.Now().Add(2 * time.Second)
	var got loginView
	for time.Now().Before(deadline) {
		res, body = doJSON(t, srv, http.MethodGet, "/api/login-attempts/"+created.ID, nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET login status=%d body=%s", res.StatusCode, body)
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("get json: %v", err)
		}
		if got.State == login_session.AttemptSucceeded {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.State != login_session.AttemptSucceeded {
		t.Fatalf("state=%q, want succeeded: %+v", got.State, got)
	}
	if got.AccountID != "user_pkce" {
		t.Fatalf("account_id=%q", got.AccountID)
	}
	if got.AccountID == created.ID {
		t.Fatal("attempt id reused as account id")
	}

	res, body = doJSON(t, srv, http.MethodGet, "/api/login-attempts", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/login-attempts status=%d body=%s", res.StatusCode, body)
	}
	var list loginList
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("list json: %v", err)
	}
	if len(list.LoginAttempts) != 1 || list.LoginAttempts[0].ID != created.ID {
		t.Fatalf("list = %+v", list)
	}

	res, _ = doJSON(t, srv, http.MethodDelete, "/api/login-attempts/"+created.ID, nil)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE login status=%d", res.StatusCode)
	}
	res, _ = doJSON(t, srv, http.MethodGet, "/api/login-attempts/"+created.ID, nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET deleted login status=%d", res.StatusCode)
	}
}

func TestControlAPICreateAccountErrors(t *testing.T) {
	reject := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	t.Cleanup(reject.Close)

	store, err := login_session.NewStore(filepath.Join(t.TempDir(), "data.json"), &cursor_account_sdk.Client{
		HTTP: reject.Client(),
		Endpoints: cursor_account_sdk.Endpoints{
			RefreshURL: reject.URL,
		},
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	h := &Handler{Store: store, Attempts: &login_session.LoginAttempts{Store: store}}
	srv := httptest.NewServer(testMux(h))
	t.Cleanup(srv.Close)

	res, body := doJSON(t, srv, http.MethodPost, "/api/accounts", map[string]string{"accessToken": "only-access"})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing refresh status=%d body=%s", res.StatusCode, body)
	}

	res, body = doJSON(t, srv, http.MethodPost, "/api/accounts", map[string]string{"refreshToken": "bad"})
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("rejected refresh status=%d body=%s", res.StatusCode, body)
	}
	var failed addAccountResponse
	if err := json.Unmarshal(body, &failed); err != nil {
		t.Fatalf("error json: %v", err)
	}
	if failed.OK || failed.Error == "" {
		t.Fatalf("failed = %+v", failed)
	}

	reject.Close()
	res, body = doJSON(t, srv, http.MethodPost, "/api/accounts", map[string]string{
		"refreshToken": fakeJWT(t, "user_down", time.Now().Add(time.Hour)),
	})
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("down refresh status=%d body=%s, want 502", res.StatusCode, body)
	}
}

func TestControlAPICreateAccountNestedCursorJSON(t *testing.T) {
	refresh := fakeJWT(t, "user_nested", time.Now().Add(24*time.Hour))
	access := fakeJWT(t, "user_nested", time.Now().Add(time.Hour))
	refreshSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"accessToken":  access,
			"refreshToken": refresh,
		})
	}))
	t.Cleanup(refreshSrv.Close)

	store, err := login_session.NewStore(filepath.Join(t.TempDir(), "data.json"), &cursor_account_sdk.Client{
		HTTP: refreshSrv.Client(),
		Endpoints: cursor_account_sdk.Endpoints{
			RefreshURL: refreshSrv.URL,
		},
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	h := &Handler{Store: store, Attempts: &login_session.LoginAttempts{Store: store}}
	srv := httptest.NewServer(testMux(h))
	t.Cleanup(srv.Close)

	res, body := doJSON(t, srv, http.MethodPost, "/api/accounts", map[string]any{
		"cursor": map[string]string{"refresh": refresh},
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("nested POST status=%d body=%s", res.StatusCode, body)
	}
	var created addAccountResponse
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("create json: %v", err)
	}
	if !created.OK || created.ID != "user_nested" {
		t.Fatalf("created = %+v", created)
	}
	if strings.Contains(string(body), access) || strings.Contains(string(body), refresh) {
		t.Fatalf("tokens leaked in nested create: %s", body)
	}

	res, body = doJSON(t, srv, http.MethodGet, "/api/accounts/user_nested", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET nested account status=%d body=%s", res.StatusCode, body)
	}
	if strings.Contains(string(body), access) || strings.Contains(string(body), refresh) {
		t.Fatalf("tokens leaked in nested get: %s", body)
	}
}

func TestControlAPIMissingAndBadBodies(t *testing.T) {
	store, err := login_session.NewStore(filepath.Join(t.TempDir(), "data.json"), &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	h := &Handler{Store: store, Attempts: &login_session.LoginAttempts{Store: store}}
	srv := httptest.NewServer(testMux(h))
	t.Cleanup(srv.Close)

	res, body := doJSON(t, srv, http.MethodGet, "/api/login-attempts/missing", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET missing login status=%d body=%s", res.StatusCode, body)
	}
	assertErrorBody(t, body, "login attempt not found")

	res, body = doJSON(t, srv, http.MethodDelete, "/api/login-attempts/missing", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE missing login status=%d body=%s", res.StatusCode, body)
	}
	assertErrorBody(t, body, "login attempt not found")

	res, body = doJSON(t, srv, http.MethodGet, "/api/accounts/missing", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET missing account status=%d body=%s", res.StatusCode, body)
	}
	assertErrorBody(t, body, "account not found")

	res, body = doJSON(t, srv, http.MethodDelete, "/api/accounts/missing", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE missing account status=%d body=%s", res.StatusCode, body)
	}
	assertErrorBody(t, body, "account not found")

	res, body = doJSON(t, srv, http.MethodPost, "/api/accounts", nil)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty body status=%d body=%s", res.StatusCode, body)
	}
	assertCreateError(t, body, errEmptyBody.Error())

	res, body = doRaw(t, srv, http.MethodPost, "/api/accounts", "application/json", []byte("{"))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad json status=%d body=%s", res.StatusCode, body)
	}
	assertCreateError(t, body, "unexpected end of JSON input")

	res, body = doRaw(t, srv, http.MethodPost, "/api/accounts", "application/json", bytes.Repeat([]byte("a"), maxBodyBytes+1))
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("too large status=%d body=%s", res.StatusCode, body)
	}
	assertCreateError(t, body, errBodyTooLarge.Error())

	res, _ = doJSON(t, srv, http.MethodPost, "/api/status", nil)
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/status status=%d, want 405", res.StatusCode)
	}

	res, _ = doJSON(t, srv, http.MethodPut, "/api/accounts", map[string]string{"refreshToken": "x"})
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("PUT /api/accounts status=%d, want 405", res.StatusCode)
	}
}

func TestControlAPILoginAttemptFailed(t *testing.T) {
	store, err := login_session.NewStore(filepath.Join(t.TempDir(), "data.json"), &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	attempts := &login_session.LoginAttempts{
		Store:          store,
		MaxOpen:        1,
		AttemptTimeout: time.Minute,
		Keep:           time.Minute,
		Poll: func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
			return cursor_account_sdk.Credentials{}, errors.New("poll exploded")
		},
	}
	t.Cleanup(attempts.Stop)

	h := &Handler{Store: store, Attempts: attempts, MaxLoginAttempts: 1}
	srv := httptest.NewServer(testMux(h))
	t.Cleanup(srv.Close)

	res, body := doJSON(t, srv, http.MethodPost, "/api/login-attempts", nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/login-attempts status=%d body=%s", res.StatusCode, body)
	}
	var created loginView
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("create json: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var got loginView
	for time.Now().Before(deadline) {
		res, body = doJSON(t, srv, http.MethodGet, "/api/login-attempts/"+created.ID, nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET login status=%d body=%s", res.StatusCode, body)
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("get json: %v", err)
		}
		if got.State == login_session.AttemptFailed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.State != login_session.AttemptFailed {
		t.Fatalf("state=%q, want failed: %+v", got.State, got)
	}
	if got.Error == "" {
		t.Fatal("expected error on failed attempt")
	}

	res, body = doJSON(t, srv, http.MethodGet, "/api/status", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/status status=%d body=%s", res.StatusCode, body)
	}
	var state serviceState
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatalf("service json: %v", err)
	}
	if state.LoginAttempts != 1 {
		t.Fatalf("login_attempts=%d, want 1", state.LoginAttempts)
	}
}

func TestControlAPIUnconfigured(t *testing.T) {
	h := &Handler{}
	srv := httptest.NewServer(testMux(h))
	t.Cleanup(srv.Close)

	res, body := doJSON(t, srv, http.MethodGet, "/api/status", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/status status=%d body=%s", res.StatusCode, body)
	}
	var state serviceState
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatalf("service json: %v", err)
	}
	if state.Accounts != 0 || state.LoginAttempts != 0 || state.MaxLoginAttempts != 3 || state.LoginAttemptMins != 3 || state.LoginKeepMins != 5 {
		t.Fatalf("unconfigured service = %+v", state)
	}

	for _, tc := range []struct {
		method, path, want string
	}{
		{http.MethodGet, "/api/accounts", "account store is not configured"},
		{http.MethodPost, "/api/accounts", "account store is not configured"},
		{http.MethodGet, "/api/accounts/x", "account store is not configured"},
		{http.MethodDelete, "/api/accounts/x", "account store is not configured"},
		{http.MethodGet, "/api/login-attempts", "login attempts are not configured"},
		{http.MethodPost, "/api/login-attempts", "login attempts are not configured"},
		{http.MethodGet, "/api/login-attempts/x", "login attempts are not configured"},
		{http.MethodDelete, "/api/login-attempts/x", "login attempts are not configured"},
	} {
		res, body := doJSON(t, srv, tc.method, tc.path, nil)
		if res.StatusCode != http.StatusInternalServerError {
			t.Fatalf("%s %s status=%d body=%s, want 500", tc.method, tc.path, res.StatusCode, body)
		}
		assertErrorBody(t, body, tc.want)
	}
}

func testMux(h *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	h.Mount(mux)
	return mux
}

func doJSON(t *testing.T, srv *httptest.Server, method, path string, payload any) (*http.Response, []byte) {
	t.Helper()
	var data []byte
	if payload != nil {
		var err error
		data, err = json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
	}
	contentType := ""
	if payload != nil {
		contentType = "application/json"
	}
	return doRaw(t, srv, method, path, contentType, data)
}

func doRaw(t *testing.T, srv *httptest.Server, method, path, contentType string, data []byte) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if data != nil {
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, srv.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, body
}

func assertErrorBody(t *testing.T, body []byte, want string) {
	t.Helper()
	var got errorBody
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("error json: %v body=%s", err, body)
	}
	if got.Error != want {
		t.Fatalf("error=%q, want %q body=%s", got.Error, want, body)
	}
}

func assertCreateError(t *testing.T, body []byte, want string) {
	t.Helper()
	var got addAccountResponse
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("create error json: %v body=%s", err, body)
	}
	if got.OK || got.Error != want {
		t.Fatalf("create error=%+v, want %q", got, want)
	}
}

func fakeJWT(t *testing.T, sub string, exp time.Time) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, err := json.Marshal(map[string]any{"sub": sub, "exp": exp.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}
