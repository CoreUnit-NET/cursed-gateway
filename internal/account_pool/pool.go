package account_pool

/*
Package account_pool picks healthy Cursor accounts for upstream requests.

Prefer Pro over Free when enabled, rotate through healthy sessions, and
cool down accounts that hit rate limits.
*/

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/CoreUnit-NET/cursed-gateway/internal/login_session"
	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

// Pool rotates accounts with optional Pro preference and cooldowns.
type Pool struct {
	Store        *login_session.Store
	PreferPro    bool
	CooldownMins int
	MaxRetries   int

	mu       sync.Mutex
	rr       int
	cooldown map[string]time.Time // account id → cool until
	lastUsed map[string]time.Time
}

// New creates a pool bound to a session store.
func New(store *login_session.Store, preferPro bool, cooldownMins, maxRetries int) *Pool {
	if maxRetries < 1 {
		maxRetries = 1
	}
	return &Pool{
		Store:        store,
		PreferPro:    preferPro,
		CooldownMins: cooldownMins,
		MaxRetries:   maxRetries,
		cooldown:     map[string]time.Time{},
		lastUsed:     map[string]time.Time{},
	}
}

// EnsureAccess refreshes tokens for id if needed.
func (p *Pool) EnsureAccess(ctx context.Context, id string) (*cursor_account_sdk.Account, error) {
	return p.Store.EnsureAccess(ctx, id)
}

// MarkRateLimited cools an account down.
func (p *Pool) MarkRateLimited(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	mins := p.CooldownMins
	if mins < 1 {
		mins = 15
	}
	p.cooldown[id] = time.Now().Add(time.Duration(mins) * time.Minute)
}

// MarkUsed records successful use for rotation fairness.
func (p *Pool) MarkUsed(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastUsed[id] = time.Now()
}

// PickCandidates returns up to MaxRetries account ids to try, Pro-first when enabled.
func (p *Pool) PickCandidates() []*cursor_account_sdk.Account {
	list := p.Store.List()
	if len(list) == 0 {
		return nil
	}

	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()

	healthy := make([]*cursor_account_sdk.Account, 0, len(list))
	for _, a := range list {
		if until, ok := p.cooldown[a.ID]; ok && now.Before(until) {
			continue
		}
		healthy = append(healthy, a)
	}
	if len(healthy) == 0 {
		// All cooling — allow any account rather than fail hard.
		healthy = append(healthy, list...)
	}

	if p.PreferPro {
		pro := filterTier(healthy, true)
		free := filterTier(healthy, false)
		healthy = append(pro, free...)
	}

	// Rotate starting offset so we don't always burn the first account.
	if len(healthy) > 0 {
		p.rr = (p.rr + 1) % len(healthy)
		healthy = append(healthy[p.rr:], healthy[:p.rr]...)
	}

	n := p.MaxRetries
	if n > len(healthy) {
		n = len(healthy)
	}
	return healthy[:n]
}

func filterTier(in []*cursor_account_sdk.Account, wantPro bool) []*cursor_account_sdk.Account {
	var out []*cursor_account_sdk.Account
	for _, a := range in {
		if isPro(a.Tier) == wantPro {
			out = append(out, a)
		}
	}
	return out
}

func isPro(tier string) bool {
	t := strings.ToLower(strings.TrimSpace(tier))
	switch t {
	case "pro", "pro_plus", "pro+", "business", "enterprise", "ultra":
		return true
	default:
		return false
	}
}
