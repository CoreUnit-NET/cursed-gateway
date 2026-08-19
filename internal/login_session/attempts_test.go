package login_session

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

func TestLoginAttemptsCreateCapAndDelete(t *testing.T) {
	attempts := &LoginAttempts{
		MaxOpen:        2,
		AttemptTimeout: time.Minute,
		Keep:           time.Minute,
		Poll: func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
			<-ctx.Done()
			return cursor_account_sdk.Credentials{}, ctx.Err()
		},
	}
	t.Cleanup(attempts.Stop)

	first, err := attempts.Create()
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	if first.ID == "" || first.URL == "" || first.State != AttemptPending {
		t.Fatalf("first = %+v", first)
	}
	if first.AccountID != "" {
		t.Fatalf("pending attempt leaked account id %q", first.AccountID)
	}

	if _, err := attempts.Create(); err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if _, err := attempts.Create(); !errors.Is(err, ErrMaxLoginAttempts) {
		t.Fatalf("Create third err=%v, want ErrMaxLoginAttempts", err)
	}
	if attempts.OpenCount() != 2 {
		t.Fatalf("OpenCount=%d, want 2", attempts.OpenCount())
	}

	if err := attempts.Delete(first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := attempts.Get(first.ID); !errors.Is(err, ErrAttemptNotFound) {
		t.Fatalf("Get deleted err=%v", err)
	}
	if _, err := attempts.Create(); err != nil {
		t.Fatalf("Create after delete: %v", err)
	}
}

func TestLoginAttemptsSuccessStoresAccountID(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "data.json"), &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	access := fakeJWT(t, "user_login", time.Now().Add(time.Hour))
	refresh := fakeJWT(t, "user_login", time.Now().Add(24*time.Hour))
	done := make(chan struct{})
	attempts := &LoginAttempts{
		Store:          store,
		MaxOpen:        3,
		AttemptTimeout: time.Minute,
		Keep:           time.Minute,
		Poll: func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
			close(done)
			return cursor_account_sdk.Credentials{
				Access:    access,
				Refresh:   refresh,
				ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
			}, nil
		},
	}
	t.Cleanup(attempts.Stop)

	created, err := attempts.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := waitAttempt(t, attempts, created.ID, AttemptSucceeded)
	if got.AccountID != "user_login" {
		t.Fatalf("AccountID=%q, want user_login", got.AccountID)
	}
	if got.AccountID == got.ID {
		t.Fatal("attempt id reused as account id")
	}
	if got.Error != "" {
		t.Fatalf("unexpected error %q", got.Error)
	}

	listed := store.List()
	if len(listed) != 1 || PublicAccountID(listed[0]) != "user_login" {
		t.Fatalf("store = %#v", listed)
	}
	select {
	case <-done:
	default:
		t.Fatal("poll was not called")
	}
}

func TestLoginAttemptsExpireAndKeepWindow(t *testing.T) {
	var mu sync.Mutex
	now := time.Now()
	attempts := &LoginAttempts{
		MaxOpen:        3,
		AttemptTimeout: time.Minute,
		Keep:           time.Minute,
		Now: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return now
		},
		Poll: func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
			<-ctx.Done()
			return cursor_account_sdk.Credentials{}, ctx.Err()
		},
	}
	t.Cleanup(attempts.Stop)

	created, err := attempts.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	mu.Lock()
	now = now.Add(time.Minute)
	mu.Unlock()

	got, err := attempts.Get(created.ID)
	if err != nil {
		t.Fatalf("Get after timeout: %v", err)
	}
	if got.State != AttemptExpired {
		t.Fatalf("state=%q, want %s", got.State, AttemptExpired)
	}

	mu.Lock()
	now = now.Add(time.Minute)
	mu.Unlock()

	if _, err := attempts.Get(created.ID); !errors.Is(err, ErrAttemptNotFound) {
		t.Fatalf("Get after keep err=%v, want not found", err)
	}
	if len(attempts.List()) != 0 {
		t.Fatalf("list after keep = %#v", attempts.List())
	}
}

func TestLoginAttemptsStopForgetsOpen(t *testing.T) {
	attempts := &LoginAttempts{
		Poll: func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
			<-ctx.Done()
			return cursor_account_sdk.Credentials{}, ctx.Err()
		},
	}
	if _, err := attempts.Create(); err != nil {
		t.Fatalf("Create: %v", err)
	}
	attempts.Stop()
	if len(attempts.List()) != 0 {
		t.Fatalf("list after Stop = %#v", attempts.List())
	}
}

func TestLoginAttemptsFailedPoll(t *testing.T) {
	attempts := &LoginAttempts{
		MaxOpen:        1,
		AttemptTimeout: time.Minute,
		Keep:           time.Minute,
		Poll: func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
			return cursor_account_sdk.Credentials{}, errors.New("poll exploded")
		},
	}
	t.Cleanup(attempts.Stop)

	created, err := attempts.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := waitAttempt(t, attempts, created.ID, AttemptFailed)
	if got.Error == "" {
		t.Fatal("expected error on failed attempt")
	}
	if got.AccountID != "" {
		t.Fatalf("failed attempt leaked account id %q", got.AccountID)
	}
}

func TestLoginAttemptsSuccessWithoutStore(t *testing.T) {
	access := fakeJWT(t, "user_nostore", time.Now().Add(time.Hour))
	refresh := fakeJWT(t, "user_nostore", time.Now().Add(24*time.Hour))
	attempts := &LoginAttempts{
		MaxOpen:        1,
		AttemptTimeout: time.Minute,
		Keep:           time.Minute,
		Poll: func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
			return cursor_account_sdk.Credentials{
				Access:    access,
				Refresh:   refresh,
				ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
			}, nil
		},
	}
	t.Cleanup(attempts.Stop)

	created, err := attempts.Create()
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := waitAttempt(t, attempts, created.ID, AttemptFailed)
	if got.Error != "missing store" {
		t.Fatalf("error=%q, want missing store", got.Error)
	}
	if got.AccountID != "" {
		t.Fatalf("failed attempt leaked account id %q", got.AccountID)
	}
}

func TestLoginAttemptsMissingGetAndDelete(t *testing.T) {
	attempts := &LoginAttempts{
		Poll: func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
			<-ctx.Done()
			return cursor_account_sdk.Credentials{}, ctx.Err()
		},
	}
	t.Cleanup(attempts.Stop)

	if _, err := attempts.Get("missing"); !errors.Is(err, ErrAttemptNotFound) {
		t.Fatalf("Get missing err=%v, want ErrAttemptNotFound", err)
	}
	if err := attempts.Delete("missing"); !errors.Is(err, ErrAttemptNotFound) {
		t.Fatalf("Delete missing err=%v, want ErrAttemptNotFound", err)
	}
	if _, err := attempts.Get(""); !errors.Is(err, ErrAttemptNotFound) {
		t.Fatalf("Get empty err=%v, want ErrAttemptNotFound", err)
	}
}

func waitAttempt(t *testing.T, a *LoginAttempts, id, state string) Attempt {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last Attempt
	for time.Now().Before(deadline) {
		got, err := a.Get(id)
		if err == nil {
			last = got
			if got.State == state {
				return got
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("attempt %s did not reach %s, last=%+v", id, state, last)
	return Attempt{}
}
