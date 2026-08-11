package cmd_handler

/*
Sessions subcommand: list (and optionally --check) stored sessions.
Statuses with --check: valid | invalid | error: <message> (README).
*/

import (
	"context"
	"errors"
	"fmt"

	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

// Sessions lists stored sessions; with --check, validates against Cursor.
func Sessions(ctx context.Context, s *settings.Settings, rt *Runtime) error {
	store, err := rt.openStore(s.AuthPath)
	if err != nil {
		return err
	}
	list := store.List()
	if len(list) == 0 {
		fmt.Fprintln(rt.out(), "No sessions.")
		return nil
	}
	for _, a := range list {
		status := "local"
		if s.SessionsCheck {
			_, err := store.CheckAccess(ctx, a.ID)
			status = sessionCheckStatus(err)
			// reload after possible refresh
			if updated, getErr := store.Get(a.ID); getErr == nil {
				a = updated
			}
		}
		sub := a.Subject
		if sub == "" {
			sub = "-"
		}
		fmt.Fprintf(rt.out(), "%s\ttier=%s\tsub=%s\texpires=%d\t%s\n",
			a.ID, a.Tier, sub, a.ExpiresAt, status)
	}
	return nil
}

func sessionCheckStatus(err error) string {
	if err == nil {
		return "valid"
	}
	if errors.Is(err, cursor_account_sdk.ErrRefreshRejected) ||
		errors.Is(err, cursor_account_sdk.ErrMissingRefreshToken) {
		return "invalid"
	}
	return "error: " + err.Error()
}
