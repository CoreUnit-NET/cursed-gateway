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
		LogFormat:   "text",
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
		LogFormat:  "text",
	}, "Demo", "1.2.3", "abc", rt)
	if err == nil {
		t.Fatal("expected models error for empty auth store")
	}
	if !strings.Contains(err.Error(), "no sessions") {
		t.Fatalf("models err = %v", err)
	}
}
