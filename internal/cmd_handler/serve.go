package cmd_handler

/*
Serve subcommand: start the OpenAI-compatible HTTP proxy.
*/

import (
	"context"
	"io/fs"

	"github.com/CoreUnit-NET/cursed-gateway/internal/service"
	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
)

// Serve starts the OpenAI-compatible HTTP proxy.
func Serve(ctx context.Context, s *settings.Settings, rt *Runtime) error {
	var uiFS fs.FS
	if rt != nil {
		uiFS = rt.UI
	}
	return service.RunServe(ctx, s, rt.client(), uiFS)
}
