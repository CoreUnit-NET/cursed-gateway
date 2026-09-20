package main

/*
cursed-gateway entrypoint.

Load .env, parse config/settings, then dispatch cmd_handler subcommands
(login, logout, sessions, whoami, models, serve, import). Bare root
defaults to serve. Long-lived serve work is delegated to internal/service.
*/

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"syscall"

	"github.com/CoreUnit-NET/cursed-gateway/internal/cmd_handler"
	"github.com/CoreUnit-NET/cursed-gateway/internal/config"
	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
	"github.com/joho/godotenv"
)

//go:embed ui
var embeddedUI embed.FS

var DisplayName string = "Unset"
var ShortName string = "unset"
var Version string = "?.?.?"
var Commit string = "???????"

func uiFilesystem() fs.FS {
	fsys, err := fs.Sub(embeddedUI, "ui")
	if err != nil {
		panic("ui embed: " + err.Error())
	}
	return fsys
}

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "warning: .env: %v\n", err)
	}

	appConfig, err := config.ParseConfig(DisplayName, ShortName)
	if errors.Is(err, config.ErrHelpRequested) {
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	s, err := settings.FromAppConfig(appConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rt := &cmd_handler.Runtime{}
	if s.EnableUI {
		rt.UI = uiFilesystem()
	}

	if err := cmd_handler.Dispatch(ctx, s, DisplayName, Version, Commit, rt); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
