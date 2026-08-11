package cmd_handler

/*
Whoami subcommand: print local session identity metadata.
*/

import (
	"context"
	"fmt"

	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
)

// Whoami prints local identity metadata for stored sessions.
func Whoami(ctx context.Context, s *settings.Settings, rt *Runtime) error {
	_ = ctx
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
		fmt.Fprintf(rt.out(), "id=%s subject=%q tier=%s expires=%d\n",
			a.ID, a.Subject, a.Tier, a.ExpiresAt)
	}
	return nil
}
