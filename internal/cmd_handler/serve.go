package cmd_handler

/*
Serve subcommand: start the OpenAI-compatible HTTP proxy.
*/

import (
	"context"

	"github.com/CoreUnit-NET/cursed-gateway/internal/service"
	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
)

// Serve starts the OpenAI-compatible HTTP proxy.
func Serve(ctx context.Context, s *settings.Settings, rt *Runtime) error {
	return service.RunServe(ctx, s, rt.client())
}
