package cmd_handler

/*
Login subcommand: Cursor OAuth PKCE flow, then store access/refresh
session via login_session (file/config only — no serve process).
*/

import (
	"context"
	"log/slog"

	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
)

// Login runs Cursor OAuth PKCE and stores the session.
func Login(ctx context.Context, s *settings.Settings, rt *Runtime) error {
	store, err := rt.openStore(s.AuthPath)
	if err != nil {
		return err
	}
	account, err := store.LoginInteractive(ctx)
	if err != nil {
		return err
	}
	attrs := []any{"session", account.ID, "store", store.Path()}
	if account.Subject != "" {
		attrs = append(attrs, "sub", account.Subject)
	}
	slog.Info("logged in", attrs...)
	return nil
}
