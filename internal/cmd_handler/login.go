package cmd_handler

/*
Login subcommand: Cursor OAuth PKCE flow, then store access/refresh
session via login_session (file/config only — no serve process).
*/

import (
	"context"
	"fmt"

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
	fmt.Fprintf(rt.out(), "Logged in session %s", account.ID)
	if account.Subject != "" {
		fmt.Fprintf(rt.out(), " (sub %s)", account.Subject)
	}
	fmt.Fprintln(rt.out())
	fmt.Fprintf(rt.out(), "Store: %s\n", store.Path())
	return nil
}
