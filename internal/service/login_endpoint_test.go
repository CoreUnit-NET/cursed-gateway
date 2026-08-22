package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

func TestRunServeLoginGoneAndControlAPI(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)

	s := &settings.Settings{
		Host:             "127.0.0.1",
		Port:             port,
		AuthPath:         filepath.Join(dir, "data.json"),
		MaxRetries:       1,
		CooldownMins:     10,
		MaxLoginAttempts: 3,
		LoginAttemptMins: 3,
		LoginKeepMins:    5,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunServe(ctx, s, &cursor_account_sdk.Client{})
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitHTTP(t, base+"/healthz", http.StatusOK)

	client := &http.Client{Timeout: 5 * time.Second}

	res, err := client.Get(base + "/login")
	if err != nil {
		t.Fatalf("GET /login: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /login status=%d body=%s, want 404", res.StatusCode, body)
	}
	if !strings.Contains(string(body), `"error":"not found"`) {
		t.Fatalf("GET /login body=%s, want JSON not found", body)
	}

	res, err = client.Get(base + "/ai/v1/models")
	if err != nil {
		t.Fatalf("GET /ai/v1/models: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("GET /ai/v1/models status=%d, want 502 (empty store)", res.StatusCode)
	}

	res, err = client.Get(base + "/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("GET /v1/models status=%d, want 502 (empty store)", res.StatusCode)
	}

	res, err = client.Get(base + "/ai/v1/chat/completions")
	if err != nil {
		t.Fatalf("GET /ai/v1/chat/completions: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /ai/v1/chat/completions status=%d body=%s, want 405 (POST mounted)", res.StatusCode, body)
	}
	if !strings.Contains(string(body), `"code":"method_not_allowed"`) {
		t.Fatalf("GET /ai/v1/chat/completions body=%s, want JSON method_not_allowed", body)
	}

	postChat, err := http.NewRequest(http.MethodPost, base+"/ai/v1/chat/completions", strings.NewReader(`{"model":"x"}`))
	if err != nil {
		t.Fatalf("POST /ai/v1/chat/completions: %v", err)
	}
	postChat.Header.Set("Content-Type", "application/json")
	res, err = client.Do(postChat)
	if err != nil {
		t.Fatalf("POST /ai/v1/chat/completions: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /ai/v1/chat/completions status=%d body=%s, want 400", res.StatusCode, body)
	}

	postComp, err := http.NewRequest(http.MethodPost, base+"/ai/v1/completions", strings.NewReader(`{"model":"x"}`))
	if err != nil {
		t.Fatalf("POST /ai/v1/completions: %v", err)
	}
	postComp.Header.Set("Content-Type", "application/json")
	res, err = client.Do(postComp)
	if err != nil {
		t.Fatalf("POST /ai/v1/completions: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /ai/v1/completions status=%d body=%s, want 400", res.StatusCode, body)
	}

	res, err = client.Get(base + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/status status=%d body=%s", res.StatusCode, body)
	}
	var state struct {
		Accounts         int `json:"accounts"`
		LoginAttempts    int `json:"login_attempts"`
		MaxLoginAttempts int `json:"max_login_attempts"`
		LoginAttemptMins int `json:"login_attempt_mins"`
		LoginKeepMins    int `json:"login_keep_mins"`
	}
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatalf("GET /api/status json: %v", err)
	}
	if state.Accounts != 0 || state.LoginAttempts != 0 {
		t.Fatalf("unexpected counts: %+v", state)
	}
	if state.MaxLoginAttempts != 3 || state.LoginAttemptMins != 3 || state.LoginKeepMins != 5 {
		t.Fatalf("unexpected limits: %+v", state)
	}

	res, err = client.Get(base + "/api/accounts")
	if err != nil {
		t.Fatalf("GET /api/accounts: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/accounts status=%d body=%s", res.StatusCode, body)
	}
	var accounts struct {
		Accounts []json.RawMessage `json:"accounts"`
	}
	if err := json.Unmarshal(body, &accounts); err != nil {
		t.Fatalf("GET /api/accounts json: %v", err)
	}
	if len(accounts.Accounts) != 0 {
		t.Fatalf("accounts=%s, want empty", body)
	}

	res, err = client.Get(base + "/api/login-attempts")
	if err != nil {
		t.Fatalf("GET /api/login-attempts: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/login-attempts status=%d body=%s", res.StatusCode, body)
	}
	var login struct {
		LoginAttempts []json.RawMessage `json:"login_attempts"`
	}
	if err := json.Unmarshal(body, &login); err != nil {
		t.Fatalf("GET /api/login-attempts json: %v", err)
	}
	if len(login.LoginAttempts) != 0 {
		t.Fatalf("login_attempts=%s, want empty", body)
	}

	res, err = client.Get(base + "/api/accounts/missing")
	if err != nil {
		t.Fatalf("GET /api/accounts/missing: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /api/accounts/missing status=%d, want 404", res.StatusCode)
	}

	res, err = client.Get(base + "/models")
	if err != nil {
		t.Fatalf("GET /models: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("GET /models status=%d, want 502 (empty store)", res.StatusCode)
	}

	res, err = client.Get(base + "/chat/completions")
	if err != nil {
		t.Fatalf("GET /chat/completions: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /chat/completions status=%d, want 405", res.StatusCode)
	}

	res, err = client.Get(base + "/completions")
	if err != nil {
		t.Fatalf("GET /completions: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /completions status=%d, want 405", res.StatusCode)
	}

	postModels, err := http.NewRequest(http.MethodPost, base+"/ai/v1/models", nil)
	if err != nil {
		t.Fatalf("POST /ai/v1/models: %v", err)
	}
	res, err = client.Do(postModels)
	if err != nil {
		t.Fatalf("POST /ai/v1/models: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /ai/v1/models status=%d, want 405", res.StatusCode)
	}

	res, err = client.Get(base + "/ai/v1/nope")
	if err != nil {
		t.Fatalf("GET /ai/v1/nope: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /ai/v1/nope status=%d body=%s, want 404", res.StatusCode, body)
	}
	if !strings.Contains(string(body), `"code":"not_found"`) {
		t.Fatalf("GET /ai/v1/nope body=%s, want JSON not_found", body)
	}

	postEmpty, err := http.NewRequest(http.MethodPost, base+"/v1/chat/completions", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST empty chat: %v", err)
	}
	postEmpty.Header.Set("Content-Type", "application/json")
	res, err = client.Do(postEmpty)
	if err != nil {
		t.Fatalf("POST empty chat: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST empty chat status=%d body=%s, want 400", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "messages is required") {
		t.Fatalf("POST empty chat body=%s, want messages is required", body)
	}

	postSystem, err := http.NewRequest(http.MethodPost, base+"/chat/completions", strings.NewReader(`{"model":"x","messages":[{"role":"system","content":"only"}]}`))
	if err != nil {
		t.Fatalf("POST system-only: %v", err)
	}
	postSystem.Header.Set("Content-Type", "application/json")
	res, err = client.Do(postSystem)
	if err != nil {
		t.Fatalf("POST system-only: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST system-only status=%d body=%s, want 400", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "No user message found") {
		t.Fatalf("POST system-only body=%s, want No user message found", body)
	}

	postAPI, err := http.NewRequest(http.MethodPost, base+"/api/status", nil)
	if err != nil {
		t.Fatalf("POST /api/status: %v", err)
	}
	res, err = client.Do(postAPI)
	if err != nil {
		t.Fatalf("POST /api/status: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/status status=%d body=%s, want 405", res.StatusCode, body)
	}
	if !strings.Contains(string(body), `"error":"method not allowed"`) {
		t.Fatalf("POST /api/status body=%s, want JSON method not allowed", body)
	}

	putAccounts, err := http.NewRequest(http.MethodPut, base+"/api/accounts", strings.NewReader(`{"refreshToken":"x"}`))
	if err != nil {
		t.Fatalf("PUT /api/accounts: %v", err)
	}
	putAccounts.Header.Set("Content-Type", "application/json")
	res, err = client.Do(putAccounts)
	if err != nil {
		t.Fatalf("PUT /api/accounts: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("PUT /api/accounts status=%d, want 405", res.StatusCode)
	}

	res, err = client.Get(base + "/v1/unknown")
	if err != nil {
		t.Fatalf("GET /v1/unknown: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /v1/unknown status=%d, want 404", res.StatusCode)
	}

	postEmptyComp, err := http.NewRequest(http.MethodPost, base+"/completions", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST empty completions: %v", err)
	}
	postEmptyComp.Header.Set("Content-Type", "application/json")
	res, err = client.Do(postEmptyComp)
	if err != nil {
		t.Fatalf("POST empty completions: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST empty completions status=%d body=%s, want 400", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "prompt is required") {
		t.Fatalf("POST empty completions body=%s, want prompt is required", body)
	}

	postBadChat, err := http.NewRequest(http.MethodPost, base+"/chat/completions", strings.NewReader(`{`))
	if err != nil {
		t.Fatalf("POST bad chat json: %v", err)
	}
	postBadChat.Header.Set("Content-Type", "application/json")
	res, err = client.Do(postBadChat)
	if err != nil {
		t.Fatalf("POST bad chat json: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST bad chat json status=%d body=%s, want 400", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "invalid JSON") {
		t.Fatalf("POST bad chat json body=%s, want invalid JSON", body)
	}

	postEmptyAccounts, err := http.NewRequest(http.MethodPost, base+"/api/accounts", nil)
	if err != nil {
		t.Fatalf("POST empty accounts: %v", err)
	}
	res, err = client.Do(postEmptyAccounts)
	if err != nil {
		t.Fatalf("POST empty accounts: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST empty accounts status=%d body=%s, want 400", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "request body is empty") {
		t.Fatalf("POST empty accounts body=%s, want request body is empty", body)
	}

	res, err = client.Get(base + "/api/login-attempts/missing")
	if err != nil {
		t.Fatalf("GET /api/login-attempts/missing: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /api/login-attempts/missing status=%d body=%s, want 404", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "login attempt not found") {
		t.Fatalf("GET /api/login-attempts/missing body=%s, want login attempt not found", body)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("RunServe: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunServe did not exit")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitHTTP(t *testing.T, url string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		res, err := http.Get(url)
		if err == nil {
			res.Body.Close()
			if res.StatusCode == want {
				return
			}
			last = fmt.Errorf("status=%d", res.StatusCode)
		} else {
			last = err
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("wait %s: %v", url, last)
}
