package login_session

import (
	"context"
	"log/slog"
	"sort"
	"time"

	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

const (
	defaultRefreshMargin = time.Minute
	fastRefreshHorizon   = 10 * time.Minute
	minRefreshSpacing    = 5 * time.Second
)

// StartRefreshLoops runs boot fast-refresh then a staggered background loop.
// It blocks until ctx is cancelled.
func (s *Store) StartRefreshLoops(ctx context.Context, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	s.bootFastRefresh(ctx, log)
	s.staggeredRefreshLoop(ctx, log)
}

func (s *Store) bootFastRefresh(ctx context.Context, log *slog.Logger) {
	now := time.Now()
	list := s.List()
	type item struct {
		id      string
		expires int64
	}
	var due []item
	for _, a := range list {
		if a.Refresh == "" {
			continue
		}
		if a.NeedsRefresh(now) || (a.ExpiresAt > 0 && a.ExpiresAt-now.UnixMilli() <= fastRefreshHorizon.Milliseconds()) {
			due = append(due, item{id: a.ID, expires: a.ExpiresAt})
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].expires < due[j].expires })
	for _, d := range due {
		if ctx.Err() != nil {
			return
		}
		if _, err := s.EnsureAccess(ctx, d.id); err != nil {
			log.Warn("boot refresh failed", "session", d.id, "err", err)
			continue
		}
		log.Info("boot refresh ok", "session", d.id)
	}
}

func (s *Store) staggeredRefreshLoop(ctx context.Context, log *slog.Logger) {
	for {
		if ctx.Err() != nil {
			return
		}
		wait := s.nextRefreshWait()
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		id := s.pickOldestRefresh()
		if id == "" {
			continue
		}
		if _, err := s.EnsureAccess(ctx, id); err != nil {
			log.Warn("staggered refresh failed", "session", id, "err", err)
			continue
		}
		log.Debug("staggered refresh ok", "session", id)
	}
}

func (s *Store) nextRefreshWait() time.Duration {
	list := s.List()
	n := 0
	for _, a := range list {
		if a.Refresh != "" {
			n++
		}
	}
	if n == 0 {
		return 30 * time.Second
	}

	// Estimate remaining lifetime from the soonest expiry.
	now := time.Now().UnixMilli()
	var soonest int64
	for _, a := range list {
		if a.Refresh == "" || a.ExpiresAt <= 0 {
			continue
		}
		if soonest == 0 || a.ExpiresAt < soonest {
			soonest = a.ExpiresAt
		}
	}
	remain := time.Duration(soonest-now)*time.Millisecond - defaultRefreshMargin
	if remain < time.Minute {
		remain = time.Minute
	}
	spacing := remain / time.Duration(n)
	if spacing < minRefreshSpacing {
		spacing = minRefreshSpacing
	}
	return spacing
}

func (s *Store) pickOldestRefresh() string {
	list := s.List()
	var best *cursor_account_sdk.Account
	for _, a := range list {
		if a.Refresh == "" {
			continue
		}
		if best == nil || a.LastRefreshAt < best.LastRefreshAt {
			best = a
		}
	}
	if best == nil {
		return ""
	}
	return best.ID
}
