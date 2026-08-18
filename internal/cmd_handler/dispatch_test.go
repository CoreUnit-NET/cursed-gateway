package cmd_handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CoreUnit-NET/cursed-gateway/internal/config"
	"github.com/CoreUnit-NET/cursed-gateway/internal/login_session"
	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

func TestSessionCheckStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want string
	}{
		{nil, "valid"},
		{cursor_account_sdk.ErrRefreshRejected, "invalid"},
		{fmt.Errorf("%w: HTTP 401", cursor_account_sdk.ErrRefreshRejected), "invalid"},
		{cursor_account_sdk.ErrMissingRefreshToken, "invalid"},
		{cursor_account_sdk.ErrRefreshTransient, "error: cursor token refresh failed"},
		{fmt.Errorf("%w: boom", cursor_account_sdk.ErrRefreshTransient), "error: cursor token refresh failed: boom"},
		{errors.New("disk full"), "error: disk full"},
	}
	for _, tc := range cases {
		if got := sessionCheckStatus(tc.err); got != tc.want {
			t.Fatalf("sessionCheckStatus(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestDispatchVersionAndStubs(t *testing.T) {
	var out bytes.Buffer
	rt := &Runtime{Out: &out, Err: &out}

	err := Dispatch(context.Background(), &settings.Settings{
		ShowVersion: true,
		Host:        "127.0.0.1",
		Port:        8080,
		AuthPath:    filepath.Join(t.TempDir(), "data.json"),
	}, "Demo", "1.2.3", "abc", rt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Demo 1.2.3 (abc)") {
		t.Fatalf("version output: %q", out.String())
	}

	err = Dispatch(context.Background(), &settings.Settings{
		Command:    config.CommandModels,
		Host:       "127.0.0.1",
		Port:       8080,
		AuthPath:   filepath.Join(t.TempDir(), "data.json"),
		MaxRetries: 1,
	}, "Demo", "1.2.3", "abc", rt)
	if err == nil {
		t.Fatal("expected models error for empty auth store")
	}
	if !strings.Contains(err.Error(), "no sessions") {
		t.Fatalf("models err = %v", err)
	}
}

func TestSessionsAndLogoutUsePublicAccountID(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "data.json")
	store, err := login_session.NewStore(authPath, &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	acc := &cursor_account_sdk.Account{
		ID:        "store-uuid",
		Subject:   "user_cli",
		Tier:      "pro",
		Access:    "tok",
		Refresh:   "ref",
		ExpiresAt: 123,
	}
	if err := store.Add(acc); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var out bytes.Buffer
	rt := &Runtime{Out: &out, Err: &out}
	s := &settings.Settings{AuthPath: authPath}
	if err := Sessions(context.Background(), s, rt); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "user_cli\t") {
		t.Fatalf("sessions output = %q, want public id first", got)
	}
	if strings.Contains(got, "store-uuid") {
		t.Fatalf("sessions printed store uuid: %q", got)
	}

	out.Reset()
	s.Args = []string{"user_cli"}
	if err := Logout(context.Background(), s, rt); err != nil {
		t.Fatal(err)
	}
	reloaded, err := login_session.NewStore(authPath, &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if n := len(reloaded.List()); n != 0 {
		t.Fatalf("logout by public id left %d session(s)", n)
	}
}

func TestLogoutEmptyRemovesAll(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "data.json")
	store, err := login_session.NewStore(authPath, &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for i, id := range []string{"a", "b"} {
		acc := &cursor_account_sdk.Account{
			ID:      "store-" + id,
			Subject: "user_" + id,
			Access:  "tok",
			Refresh: "ref",
		}
		if err := store.Add(acc); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}

	var out bytes.Buffer
	rt := &Runtime{Out: &out, Err: &out}
	if err := Logout(context.Background(), &settings.Settings{AuthPath: authPath}, rt); err != nil {
		t.Fatal(err)
	}
	reloaded, err := login_session.NewStore(authPath, &cursor_account_sdk.Client{})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if n := len(reloaded.List()); n != 0 {
		t.Fatalf("empty logout left %d session(s)", n)
	}
}
