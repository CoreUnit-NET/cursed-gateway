package cmdHandler

/*
Package cmdHandler implements CLI subcommand handlers.

Handlers receive validated settings and perform one-shot or long-lived
work. They do not parse flags; config/settings already did that.
*/

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/CoreUnit-NET/cursed-gateway/internal/config"
	login_session "github.com/CoreUnit-NET/cursed-gateway/internal/loginSession"
	"github.com/CoreUnit-NET/cursed-gateway/internal/service"
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
	return login_session.NewStore(path, r.client())
}

// Dispatch runs the selected settings.Command.
func Dispatch(ctx context.Context, s *settings.Settings, displayName, version, commit string, rt *Runtime) error {
	if s == nil {
		return fmt.Errorf("settings is nil")
	}
	if rt == nil {
		rt = &Runtime{}
	}

	if s.ShowVersion || s.Command == config.CommandVersion {
		fmt.Fprintf(rt.out(), "%s %s (%s)\n", displayName, version, commit)
		return nil
	}

	switch s.Command {
	case "":
		return fmt.Errorf("no command selected; run with --help")
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
	case config.CommandServe:
		return Serve(ctx, s, rt)
	default:
		return fmt.Errorf("unknown command %q", s.Command)
	}
}

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
	n, err := store.Remove(id)
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

// Sessions lists stored sessions; with --check, validates against Cursor.
// Statuses with --check: valid | invalid | error: <message> (README).
func Sessions(ctx context.Context, s *settings.Settings, rt *Runtime) error {
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
		status := "local"
		if s.SessionsCheck {
			_, err := store.CheckAccess(ctx, a.ID)
			status = sessionCheckStatus(err)
			// reload after possible refresh
			if updated, getErr := store.Get(a.ID); getErr == nil {
				a = updated
			}
		}
		sub := a.Subject
		if sub == "" {
			sub = "-"
		}
		fmt.Fprintf(rt.out(), "%s\ttier=%s\tsub=%s\texpires=%d\t%s\n",
			a.ID, a.Tier, sub, a.ExpiresAt, status)
	}
	return nil
}

func sessionCheckStatus(err error) string {
	if err == nil {
		return "valid"
	}
	if errors.Is(err, cursor_account_sdk.ErrRefreshRejected) ||
		errors.Is(err, cursor_account_sdk.ErrMissingRefreshToken) {
		return "invalid"
	}
	return "error: " + err.Error()
}

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

// Models fetches Cursor models (requires at least one usable session).
func Models(ctx context.Context, s *settings.Settings, rt *Runtime) error {
	return service.PrintModels(ctx, s, rt.out(), rt.client())
}

// Serve starts the OpenAI-compatible HTTP proxy.
func Serve(ctx context.Context, s *settings.Settings, rt *Runtime) error {
	return service.RunServe(ctx, s, rt.client())
}
