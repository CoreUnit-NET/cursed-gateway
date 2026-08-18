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
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /login status=%d, want 404", res.StatusCode)
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
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /ai/v1/chat/completions status=%d, want 405 (POST mounted)", res.StatusCode)
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
	body, _ := io.ReadAll(res.Body)
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

	res, err = client.Get(base + "/api")
	if err != nil {
		t.Fatalf("GET /api: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api status=%d body=%s", res.StatusCode, body)
	}
	var state struct {
		Accounts         int `json:"accounts"`
		LoginAttempts    int `json:"login_attempts"`
		MaxLoginAttempts int `json:"max_login_attempts"`
		LoginAttemptMins int `json:"login_attempt_mins"`
		LoginKeepMins    int `json:"login_keep_mins"`
	}
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatalf("GET /api json: %v", err)
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

	res, err = client.Get(base + "/api/login")
	if err != nil {
		t.Fatalf("GET /api/login: %v", err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/login status=%d body=%s", res.StatusCode, body)
	}
	var login struct {
		Login []json.RawMessage `json:"login"`
	}
	if err := json.Unmarshal(body, &login); err != nil {
		t.Fatalf("GET /api/login json: %v", err)
	}
	if len(login.Login) != 0 {
		t.Fatalf("login=%s, want empty", body)
	}

	res, err = client.Get(base + "/api/accounts/missing")
	if err != nil {
		t.Fatalf("GET /api/accounts/missing: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /api/accounts/missing status=%d, want 404", res.StatusCode)
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
