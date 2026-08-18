package cmd_handler

/*
Logout subcommand: remove sessions from the gateway store.
*/

import (
	"context"
	"fmt"

	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
)

// Logout removes one session or all sessions.
func Logout(ctx context.Context, s *settings.Settings, rt *Runtime) error {
	_ = ctx
	store, err := rt.openStore(s.AuthPath)
	if err != nil {
		return err
	}
	id := ""
	if len(s.Args) > 0 {
		id = s.Args[0]
	}
	var n int
	if id == "" {
		n, err = store.Remove("")
	} else {
		n, err = store.RemoveMatch(id)
	}
	if err != nil {
		return err
	}
	if id == "" {
		fmt.Fprintf(rt.out(), "Removed %d session(s) from %s\n", n, store.Path())
	} else {
		fmt.Fprintf(rt.out(), "Removed session %s from %s\n", id, store.Path())
	}
	return nil
}
