package cmd_handler

/*
Package cmd_handler implements CLI subcommand handlers.

Handlers receive validated settings and perform one-shot or long-lived
work. They do not parse flags; config/settings already did that.

Output policy:
  - Process events (login URL, login success, serve lifecycle) → log/slog on stderr
  - Tabular / status CLI results (sessions, whoami, models, version, import/logout) → fmt on stdout
*/

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/CoreUnit-NET/cursed-gateway/internal/applog"
	"github.com/CoreUnit-NET/cursed-gateway/internal/config"
	"github.com/CoreUnit-NET/cursed-gateway/internal/login_session"
	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

// Runtime is shared wiring for handlers (stdout/stderr, auth client).
type Runtime struct {
	Out    io.Writer
	Err    io.Writer
	Client *cursor_account_sdk.Client
}

func (r *Runtime) out() io.Writer {
	if r != nil && r.Out != nil {
		return r.Out
	}
	return os.Stdout
}

func (r *Runtime) errw() io.Writer {
	if r != nil && r.Err != nil {
		return r.Err
	}
	return os.Stderr
}

func (r *Runtime) client() *cursor_account_sdk.Client {
	if r != nil && r.Client != nil {
		return r.Client
	}
	return &cursor_account_sdk.Client{}
}

func (r *Runtime) openStore(path string) (*login_session.Store, error) {
	store, err := login_session.NewStore(path, r.client())
	if err != nil {
		return nil, err
	}
	store.Log = slog.Default()
	return store, nil
}

// Dispatch runs the selected settings.Command.
func Dispatch(ctx context.Context, s *settings.Settings, displayName, version, commit string, rt *Runtime) error {
	if s == nil {
		return fmt.Errorf("settings is nil")
	}
	if rt == nil {
		rt = &Runtime{}
	}

	slog.SetDefault(applog.New(s.Verbose))
	if s.Verbose {
		slog.Info("verbose mode enabled")
	}

	if s.ShowVersion || s.Command == config.CommandVersion {
		fmt.Fprintf(rt.out(), "%s %s (%s)\n", displayName, version, commit)
		return nil
	}

	switch s.Command {
	case "", config.CommandServe:
		// Empty command is treated as serve (root default).
		return Serve(ctx, s, rt)
	case config.CommandLogin:
		return Login(ctx, s, rt)
	case config.CommandImport:
		return Import(ctx, s, rt)
	case config.CommandLogout:
		return Logout(ctx, s, rt)
	case config.CommandSessions:
		return Sessions(ctx, s, rt)
	case config.CommandWhoami:
		return Whoami(ctx, s, rt)
	case config.CommandModels:
		return Models(ctx, s, rt)
	default:
		return fmt.Errorf("unknown command %q", s.Command)
	}
}
