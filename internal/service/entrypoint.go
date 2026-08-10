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
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	account_pool "github.com/CoreUnit-NET/cursed-gateway/internal/accountPool"
	completion_api "github.com/CoreUnit-NET/cursed-gateway/internal/completionApi"
	login_session "github.com/CoreUnit-NET/cursed-gateway/internal/loginSession"
	"github.com/CoreUnit-NET/cursed-gateway/internal/settings"
	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
	cursor_api_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/api"
)

// PrintModels fetches and prints available Cursor models.
func PrintModels(ctx context.Context, s *settings.Settings, out io.Writer, client *cursor_account_sdk.Client) error {
	if out == nil {
		out = os.Stdout
	}
	store, err := login_session.NewStore(s.AuthPath, client)
	if err != nil {
		return err
	}
	pool := account_pool.New(store, s.PreferPro, s.CooldownMins, maxRetries(s))
	api := &cursor_api_sdk.Client{}

	cands := pool.PickCandidates()
	if len(cands) == 0 {
		return fmt.Errorf("no sessions in auth store; run login or import first")
	}

	var models []cursor_api_sdk.Model
	var last error
	for _, acc := range cands {
		ready, err := pool.EnsureAccess(ctx, acc.ID)
		if err != nil {
			last = err
			continue
		}
		models, err = api.ListModels(ctx, ready.Access)
		if err != nil {
			last = err
			continue
		}
		pool.MarkUsed(acc.ID)
		last = nil
		break
	}
	if last != nil {
		return last
	}

	for _, m := range models {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		fmt.Fprintf(out, "%s\t%s\n", m.ID, name)
	}
	return nil
}

// RunServe starts the OpenAI-compatible HTTP proxy and session refresh loops.
func RunServe(ctx context.Context, s *settings.Settings, client *cursor_account_sdk.Client) error {
	log := newLogger(s)
	slog.SetDefault(log)

	store, err := login_session.NewStore(s.AuthPath, client)
	if err != nil {
		return err
	}

	refreshCtx, refreshCancel := context.WithCancel(ctx)
	defer refreshCancel()
	go store.StartRefreshLoops(refreshCtx, log)

	pool := account_pool.New(store, s.PreferPro, s.CooldownMins, maxRetries(s))
	api := &cursor_api_sdk.Client{}
	srvAPI := &completion_api.Server{
		Pool: pool,
		API:  api,
		Log:  log,
	}
	handler := &completion_api.Handler{Server: srvAPI}
	mux := http.NewServeMux()
	handler.Mount(mux)

	var pendingLogin *login_session.PendingLogin
	if s.EnableLogin {
		pendingLogin = &login_session.PendingLogin{
			Store:  store,
			Client: client,
			Log:    log,
			Parent: ctx,
		}
		mux.Handle("GET /login", pendingLogin)
		log.Info("http login endpoint enabled", "path", "/login")
	}

	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr, "auth", store.Path(), "enable_login", s.EnableLogin)
		err := httpSrv.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		if pendingLogin != nil {
			pendingLogin.Stop()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		refreshCancel()
		return <-errCh
	case err := <-errCh:
		if pendingLogin != nil {
			pendingLogin.Stop()
		}
		refreshCancel()
		return err
	}
}

func maxRetries(s *settings.Settings) int {
	if s == nil || s.MaxRetries < 1 {
		return 1
	}
	return s.MaxRetries
}

func newLogger(s *settings.Settings) *slog.Logger {
	level := slog.LevelInfo
	if s != nil {
		switch s.LogLevel {
		case "debug":
			level = slog.LevelDebug
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if s != nil && s.LogFormat == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	return slog.New(h)
}
