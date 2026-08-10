package login_session

/*
PendingLogin owns at most one in-flight HTTP PKCE login attempt.

GET /login reuses the same Deep Control URL until the background poll
succeeds, fails, times out, or Stop cancels the attempt.
*/

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

// PendingLogin serves temporary redirects to an open Cursor login URL.
type PendingLogin struct {
	Store  *Store
	Client *cursor_account_sdk.Client
	Log    *slog.Logger
	// Parent is cancelled on serve shutdown; poll inherits it.
	Parent context.Context
	// Poll overrides Client.PollAuth (tests).
	Poll func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error)

	mu   sync.Mutex
	open *openLogin
}

type openLogin struct {
	params cursor_account_sdk.AuthParams
	cancel context.CancelFunc
}

func (p *PendingLogin) log() *slog.Logger {
	if p != nil && p.Log != nil {
		return p.Log
	}
	return slog.Default()
}

func (p *PendingLogin) client() *cursor_account_sdk.Client {
	if p != nil && p.Client != nil {
		return p.Client
	}
	return &cursor_account_sdk.Client{}
}

func (p *PendingLogin) parent() context.Context {
	if p != nil && p.Parent != nil {
		return p.Parent
	}
	return context.Background()
}

func (p *PendingLogin) pollFn() func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
	if p != nil && p.Poll != nil {
		return p.Poll
	}
	client := p.client()
	return client.PollAuth
}

// EnsureRedirectURL returns the open login URL, starting a new attempt if needed.
func (p *PendingLogin) EnsureRedirectURL() (string, error) {
	if p == nil {
		return "", fmt.Errorf("pending login is nil")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.open != nil {
		return p.open.params.LoginURL, nil
	}

	params, err := p.client().GenerateAuthParams()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithCancel(p.parent())
	p.open = &openLogin{params: params, cancel: cancel}
	go p.runPoll(ctx, params)
	p.log().Info("http login attempt started", "uuid", params.UUID)
	return params.LoginURL, nil
}

func (p *PendingLogin) runPoll(ctx context.Context, params cursor_account_sdk.AuthParams) {
	defer p.finish(params.UUID)

	creds, err := p.pollFn()(ctx, params.UUID, params.Verifier)
	if err != nil {
		if ctx.Err() != nil {
			p.log().Info("http login attempt cancelled", "uuid", params.UUID)
			return
		}
		p.log().Warn("http login attempt failed", "uuid", params.UUID, "err", err)
		return
	}

	account, err := cursor_account_sdk.NewAccountFromCredentials(creds, time.Now())
	if err != nil {
		p.log().Warn("http login account build failed", "uuid", params.UUID, "err", err)
		return
	}
	if p.Store == nil {
		p.log().Warn("http login missing store", "uuid", params.UUID)
		return
	}
	if _, err := p.Store.UpsertBySubject(account); err != nil {
		p.log().Warn("http login store failed", "uuid", params.UUID, "err", err)
		return
	}
	p.log().Info("http login attempt completed", "uuid", params.UUID, "session", account.ID)
}

func (p *PendingLogin) finish(uuid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.open != nil && p.open.params.UUID == uuid {
		p.open.cancel()
		p.open = nil
	}
}

// Stop cancels any open login attempt.
func (p *PendingLogin) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.open != nil {
		p.open.cancel()
		p.open = nil
	}
}

// ServeHTTP answers GET with a 307 redirect to the Cursor login URL only.
func (p *PendingLogin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	loginURL, err := p.EnsureRedirectURL()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Location", loginURL)
	w.WriteHeader(http.StatusTemporaryRedirect)
}
