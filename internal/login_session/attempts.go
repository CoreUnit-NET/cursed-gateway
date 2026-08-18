package login_session

/*
LoginAttempts tracks concurrent Control API PKCE login attempts.

Attempt id is Cursor’s PKCE uuid and is never used as an account id.
Open attempts are capped; unanswered ones expire; resolved ones stay
listed for the keep window, then drop.
*/

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

const (
	AttemptPending   = "pending"
	AttemptSucceeded = "succeeded"
	AttemptFailed    = "failed"
	AttemptExpired   = "expired"

	defaultMaxOpen        = 3
	defaultAttemptTimeout = 3 * time.Minute
	defaultKeep           = 5 * time.Minute
)

var (
	ErrMaxLoginAttempts = errors.New("too many open login attempts")
	ErrAttemptNotFound  = errors.New("login attempt not found")
)

// Attempt is a public snapshot of one Control API login attempt.
type Attempt struct {
	ID         string
	URL        string
	State      string
	AccountID  string
	Error      string
	CreatedAt  time.Time
	ResolvedAt time.Time
}

// LoginAttempts owns in-memory PKCE attempts for the Control API.
type LoginAttempts struct {
	Store  *Store
	Client *cursor_account_sdk.Client
	Log    *slog.Logger
	Parent context.Context

	MaxOpen        int
	AttemptTimeout time.Duration
	Keep           time.Duration
	Poll           func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error)
	Now            func() time.Time

	mu    sync.Mutex
	items map[string]*loginAttempt
}

type loginAttempt struct {
	params cursor_account_sdk.AuthParams
	cancel context.CancelFunc
	view   Attempt
}

func (a *LoginAttempts) log() *slog.Logger {
	if a != nil && a.Log != nil {
		return a.Log
	}
	return slog.Default()
}

func (a *LoginAttempts) client() *cursor_account_sdk.Client {
	if a != nil && a.Client != nil {
		return a.Client
	}
	return &cursor_account_sdk.Client{}
}

func (a *LoginAttempts) parent() context.Context {
	if a != nil && a.Parent != nil {
		return a.Parent
	}
	return context.Background()
}

func (a *LoginAttempts) pollFn() func(ctx context.Context, uuid, verifier string) (cursor_account_sdk.Credentials, error) {
	if a != nil && a.Poll != nil {
		return a.Poll
	}
	return a.client().PollAuth
}

func (a *LoginAttempts) now() time.Time {
	if a != nil && a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *LoginAttempts) maxOpen() int {
	if a != nil && a.MaxOpen > 0 {
		return a.MaxOpen
	}
	return defaultMaxOpen
}

func (a *LoginAttempts) attemptTimeout() time.Duration {
	if a != nil && a.AttemptTimeout > 0 {
		return a.AttemptTimeout
	}
	return defaultAttemptTimeout
}

func (a *LoginAttempts) keep() time.Duration {
	if a != nil && a.Keep > 0 {
		return a.Keep
	}
	return defaultKeep
}

func (a *LoginAttempts) ensureItems() {
	if a.items == nil {
		a.items = map[string]*loginAttempt{}
	}
}

func (a *LoginAttempts) sweepLocked(now time.Time) {
	a.ensureItems()
	timeout := a.attemptTimeout()
	keep := a.keep()
	for id, it := range a.items {
		switch it.view.State {
		case AttemptPending:
			if timeout > 0 && now.Sub(it.view.CreatedAt) >= timeout {
				it.cancel()
				it.view.State = AttemptExpired
				it.view.ResolvedAt = now
			}
		case AttemptSucceeded, AttemptFailed, AttemptExpired:
			if keep > 0 && !it.view.ResolvedAt.IsZero() && now.Sub(it.view.ResolvedAt) >= keep {
				delete(a.items, id)
			}
		}
	}
}

func (a *LoginAttempts) openCountLocked() int {
	n := 0
	for _, it := range a.items {
		if it.view.State == AttemptPending {
			n++
		}
	}
	return n
}

func copyAttempt(v Attempt) Attempt {
	return v
}

// Create starts a new PKCE attempt. It is not an account.
func (a *LoginAttempts) Create() (Attempt, error) {
	if a == nil {
		return Attempt{}, fmt.Errorf("login attempts is nil")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sweepLocked(a.now())
	if a.openCountLocked() >= a.maxOpen() {
		return Attempt{}, ErrMaxLoginAttempts
	}

	params, err := a.client().GenerateAuthParams()
	if err != nil {
		return Attempt{}, err
	}

	ctx, cancel := context.WithTimeout(a.parent(), a.attemptTimeout())
	now := a.now()
	it := &loginAttempt{
		params: params,
		cancel: cancel,
		view: Attempt{
			ID:        params.UUID,
			URL:       params.LoginURL,
			State:     AttemptPending,
			CreatedAt: now,
		},
	}
	a.ensureItems()
	a.items[params.UUID] = it
	go a.runPoll(ctx, params.UUID, params)
	a.log().Info("control login attempt started", "id", params.UUID)
	return copyAttempt(it.view), nil
}

func (a *LoginAttempts) runPoll(ctx context.Context, id string, params cursor_account_sdk.AuthParams) {
	creds, err := a.pollFn()(ctx, params.UUID, params.Verifier)

	a.mu.Lock()
	it := a.items[id]
	if it == nil || it.view.State != AttemptPending {
		a.mu.Unlock()
		return
	}
	if err != nil {
		now := a.now()
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			it.view.State = AttemptExpired
			a.log().Info("control login attempt expired", "id", id)
		} else {
			it.view.State = AttemptFailed
			it.view.Error = err.Error()
			a.log().Warn("control login attempt failed", "id", id, "err", err)
		}
		it.view.ResolvedAt = now
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	account, storeErr := cursor_account_sdk.NewAccountFromCredentials(creds, time.Now())
	if storeErr == nil {
		if a.Store == nil {
			storeErr = fmt.Errorf("missing store")
		} else {
			_, storeErr = a.Store.UpsertBySubject(account)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	it = a.items[id]
	if it == nil || it.view.State != AttemptPending {
		return
	}
	now := a.now()
	if storeErr != nil {
		it.view.State = AttemptFailed
		it.view.Error = storeErr.Error()
		it.view.ResolvedAt = now
		a.log().Warn("control login store failed", "id", id, "err", storeErr)
		return
	}
	it.view.State = AttemptSucceeded
	it.view.AccountID = PublicAccountID(account)
	it.view.ResolvedAt = now
	a.log().Info("control login attempt completed", "id", id, "account", it.view.AccountID)
}

// List returns open attempts and resolved ones still in the keep window.
func (a *LoginAttempts) List() []Attempt {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sweepLocked(a.now())
	out := make([]Attempt, 0, len(a.items))
	for _, it := range a.items {
		out = append(out, copyAttempt(it.view))
	}
	return out
}

// Get returns one attempt by PKCE uuid.
func (a *LoginAttempts) Get(id string) (Attempt, error) {
	if a == nil {
		return Attempt{}, ErrAttemptNotFound
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sweepLocked(a.now())
	it := a.items[id]
	if it == nil {
		return Attempt{}, ErrAttemptNotFound
	}
	return copyAttempt(it.view), nil
}

// Delete closes and forgets an attempt.
func (a *LoginAttempts) Delete(id string) error {
	if a == nil {
		return ErrAttemptNotFound
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sweepLocked(a.now())
	it := a.items[id]
	if it == nil {
		return ErrAttemptNotFound
	}
	it.cancel()
	delete(a.items, id)
	a.log().Info("control login attempt closed", "id", id)
	return nil
}

// OpenCount is the number of unanswered attempts after a sweep.
func (a *LoginAttempts) OpenCount() int {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sweepLocked(a.now())
	return a.openCountLocked()
}

// Stop cancels every open attempt (serve shutdown).
func (a *LoginAttempts) Stop() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureItems()
	for id, it := range a.items {
		it.cancel()
		if it.view.State == AttemptPending {
			it.view.State = AttemptExpired
			it.view.ResolvedAt = a.now()
		}
		delete(a.items, id)
	}
}
