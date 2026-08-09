package service

/*
Package service is the app glue layer started from main.

It wires settings, login_session refresh loops, and completionApi for
long-lived serve, and can coordinate one-shot flows that need shared
init. cmdHandler calls into this package; it does not parse CLI flags.
*/

import (
	"context"
	"fmt"
	"io"

	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

// PrintModels fetches and prints available Cursor models.
// TODO: wire lib/cursor/api GetServerConfig / models listing.
func PrintModels(ctx context.Context, s *settings.Settings, out io.Writer, client *cursor_account_sdk.Client) error {
	_ = ctx
	_ = s
	_ = out
	_ = client
	return fmt.Errorf("models: not implemented yet (needs Cursor Connect client)")
}

// RunServe starts the OpenAI-compatible HTTP proxy and session refresh loops.
// TODO: wire login_session refresh + completionApi routes + Cursor AgentService/Run.
func RunServe(ctx context.Context, s *settings.Settings, client *cursor_account_sdk.Client) error {
	_ = ctx
	_ = s
	_ = client
	return fmt.Errorf("serve: not implemented yet (needs OpenAI proxy + Connect client)")
}
