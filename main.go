package main

/*
cursed-gateway entrypoint.

Load .env, parse config/settings, then dispatch cmdHandler subcommands
(login, logout, sessions, whoami, models, serve, import). Long-lived
serve work is delegated to internal/service.
*/

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/CoreUnit-NET/cursed-gateway/internal/cmdHandler"
	"github.com/CoreUnit-NET/cursed-gateway/internal/config"
	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
	"github.com/joho/godotenv"
)

var DisplayName string = "Unset"
var ShortName string = "unset"
var Version string = "?.?.?"
var Commit string = "???????"

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

	if err := cmdHandler.Dispatch(ctx, s, DisplayName, Version, Commit, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
