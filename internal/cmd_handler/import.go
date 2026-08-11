package cmd_handler

/*
Import subcommand: read ./data/auth.json (Cursor-style) and merge into
./data/data.json session store owned by login_session.
*/

import (
	"context"
	"fmt"

	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
)

// Import merges Cursor-style auth.json into the gateway store.
func Import(ctx context.Context, s *settings.Settings, rt *Runtime) error {
	_ = ctx
	store, err := rt.openStore(s.AuthPath)
	if err != nil {
		return err
	}
	account, err := store.ImportAuthFile(s.ImportPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(rt.out(), "Imported session %s from %s\n", account.ID, s.ImportPath)
	fmt.Fprintf(rt.out(), "Store: %s\n", store.Path())
	return nil
}
