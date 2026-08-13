package service

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

func TestRunServeLoginRedirectOptIn(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)

	s := &settings.Settings{
		Host:         "127.0.0.1",
		Port:         port,
		AuthPath:     filepath.Join(dir, "data.json"),
		MaxRetries:   1,
		CooldownMins: 10,
		EnableLogin:  true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunServe(ctx, s, &cursor_account_sdk.Client{})
	}()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitHTTP(t, base+"/healthz", http.StatusOK)

	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	res1, err := client.Get(base + "/login")
	if err != nil {
		t.Fatalf("GET /login: %v", err)
	}
	body1, _ := io.ReadAll(res1.Body)
	res1.Body.Close()
	if res1.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status=%d, want 307", res1.StatusCode)
	}
	loc1 := res1.Header.Get("Location")
	if loc1 == "" {
		t.Fatal("missing Location")
	}
	if len(body1) != 0 {
		t.Fatalf("expected empty body, got %q", body1)
	}

	res2, err := client.Get(base + "/login")
	if err != nil {
		t.Fatalf("GET /login reuse: %v", err)
	}
	loc2 := res2.Header.Get("Location")
	res2.Body.Close()
	if loc2 != loc1 {
		t.Fatalf("reuse mismatch:\n%s\n%s", loc1, loc2)
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

func TestRunServeLoginDisabled404(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)

	s := &settings.Settings{
		Host:         "127.0.0.1",
		Port:         port,
		AuthPath:     filepath.Join(dir, "data.json"),
		MaxRetries:   1,
		CooldownMins: 10,
		EnableLogin:  false,
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
		t.Fatalf("status=%d, want 404", res.StatusCode)
	}

	cancel()
	select {
	case <-errCh:
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
