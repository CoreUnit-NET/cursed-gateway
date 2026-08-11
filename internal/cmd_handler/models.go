package cmd_handler

/*
Models subcommand: fetch and print Cursor models for stored sessions.
*/

import (
	"context"

	"github.com/CoreUnit-NET/cursed-gateway/internal/service"
	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
)

// Models fetches Cursor models (requires at least one usable session).
func Models(ctx context.Context, s *settings.Settings, rt *Runtime) error {
	return service.PrintModels(ctx, s, rt.out(), rt.client())
}
