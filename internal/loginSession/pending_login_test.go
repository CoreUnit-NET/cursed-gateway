package login_session

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

func TestPendingLoginReusesOpenURL(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "data.json"), &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	var polls atomic.Int32

	p := &PendingLogin{
		Store:  store,
		Client: &cursor_account_sdk.Client{Endpoints: cursor_account_sdk.Endpoints{LoginURL: "https://example.test/login"}},
		Parent: context.Background(),
		Poll: func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
			polls.Add(1)
			close(started)
			select {
			case <-release:
				return cursor_account_sdk.Credentials{}, context.Canceled
			case <-ctx.Done():
				return cursor_account_sdk.Credentials{}, ctx.Err()
			}
		},
	}

	u1, err := p.EnsureRedirectURL()
	if err != nil {
		t.Fatalf("EnsureRedirectURL: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("poll did not start")
	}
	u2, err := p.EnsureRedirectURL()
	if err != nil {
		t.Fatalf("EnsureRedirectURL reuse: %v", err)
	}
	if u1 != u2 {
		t.Fatalf("urls differ:\n%s\n%s", u1, u2)
	}
	if polls.Load() != 1 {
		t.Fatalf("polls=%d, want 1", polls.Load())
	}

	close(release)
	p.Stop()
}

func TestPendingLoginNewURLAfterStop(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "data.json"), &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	block := make(chan struct{})
	p := &PendingLogin{
		Store:  store,
		Client: &cursor_account_sdk.Client{Endpoints: cursor_account_sdk.Endpoints{LoginURL: "https://example.test/login"}},
		Parent: context.Background(),
		Poll: func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
			select {
			case <-block:
				return cursor_account_sdk.Credentials{}, context.Canceled
			case <-ctx.Done():
				return cursor_account_sdk.Credentials{}, ctx.Err()
			}
		},
	}

	u1, err := p.EnsureRedirectURL()
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	p.Stop()
	// Allow finish() to clear.
	time.Sleep(20 * time.Millisecond)

	u2, err := p.EnsureRedirectURL()
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if u1 == u2 {
		t.Fatalf("expected new login url after stop, got same %q", u1)
	}
	close(block)
	p.Stop()
}

func TestPendingLoginStoresOnSuccess(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "data.json"), &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	done := make(chan struct{})
	access := fakeJWT(t, "http_user", time.Now().Add(time.Hour))
	refresh := fakeJWT(t, "http_user", time.Now().Add(24*time.Hour))

	p := &PendingLogin{
		Store:  store,
		Client: &cursor_account_sdk.Client{Endpoints: cursor_account_sdk.Endpoints{LoginURL: "https://example.test/login"}},
		Parent: context.Background(),
		Poll: func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
			defer close(done)
			return cursor_account_sdk.Credentials{
				Access:    access,
				Refresh:   refresh,
				ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
			}, nil
		},
	}

	if _, err := p.EnsureRedirectURL(); err != nil {
		t.Fatalf("EnsureRedirectURL: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("poll did not finish")
	}
	// finish() clears open after poll returns.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(store.List()) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	list := store.List()
	if len(list) != 1 {
		t.Fatalf("sessions=%d, want 1", len(list))
	}
	if list[0].Subject != "http_user" {
		t.Fatalf("subject=%q, want http_user", list[0].Subject)
	}
}

func TestPendingLoginServeHTTPRedirect(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "data.json"), &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	hold := make(chan struct{})
	p := &PendingLogin{
		Store:  store,
		Client: &cursor_account_sdk.Client{Endpoints: cursor_account_sdk.Endpoints{LoginURL: "https://example.test/login"}},
		Parent: context.Background(),
		Poll: func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
			select {
			case <-hold:
				return cursor_account_sdk.Credentials{}, context.Canceled
			case <-ctx.Done():
				return cursor_account_sdk.Credentials{}, ctx.Err()
			}
		},
	}
	t.Cleanup(func() {
		close(hold)
		p.Stop()
	})

	mux := http.NewServeMux()
	mux.Handle("GET /login", p)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	if res.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status=%d, want 307", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if loc == "" {
		t.Fatal("missing Location")
	}
	if len(body) != 0 {
		t.Fatalf("expected empty body, got %q", body)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Result().Header.Get("Location") != loc {
		t.Fatalf("reuse Location mismatch")
	}
}
