package login_session

/*
Package login_session owns auth-session persistence and token renewal.

Responsibilities:
  - load/save ./data/data.json (load returns the session list)
  - boot fast-refresh for near-expiry tokens
  - staggered refresh loop across accounts

Uses cursor_account_sdk for account/token operations. That SDK must not
manage goroutines — concurrency lives only in this package.
*/

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	cursor_account_sdk "github.com/CoreUnit-NET/cursed-gateway/lib/cursor/account"
)

var (
	ErrNotFound      = errors.New("session not found")
	ErrEmptyStore    = errors.New("no sessions in store")
	ErrInvalidImport = errors.New("invalid auth import")
)

// StoreFile is the on-disk multi-account session document.
type StoreFile struct {
	Sessions []SessionRecord `json:"sessions"`
}

// SessionRecord is the JSON shape for one gateway session.
type SessionRecord struct {
	ID            string `json:"id"`
	Label         string `json:"label,omitempty"`
	Subject       string `json:"subject,omitempty"`
	Tier          string `json:"tier,omitempty"`
	Access        string `json:"access"`
	Refresh       string `json:"refresh"`
	ExpiresAt     int64  `json:"expires"`
	LastRefreshAt int64  `json:"last_refresh_at,omitempty"`
	CreatedAt     int64  `json:"created_at,omitempty"`
	UpdatedAt     int64  `json:"updated_at,omitempty"`
}

func recordFromAccount(a *cursor_account_sdk.Account) SessionRecord {
	return SessionRecord{
		ID:            a.ID,
		Label:         a.Label,
		Subject:       a.Subject,
		Tier:          a.Tier,
		Access:        a.Access,
		Refresh:       a.Refresh,
		ExpiresAt:     a.ExpiresAt,
		LastRefreshAt: a.LastRefreshAt,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}
}

func accountFromRecord(r SessionRecord) *cursor_account_sdk.Account {
	return &cursor_account_sdk.Account{
		ID:            r.ID,
		Label:         r.Label,
		Subject:       r.Subject,
		Tier:          r.Tier,
		Access:        r.Access,
		Refresh:       r.Refresh,
		ExpiresAt:     r.ExpiresAt,
		LastRefreshAt: r.LastRefreshAt,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

// Store is a mutex-protected multi-account session file.
type Store struct {
	path   string
	mu     sync.Mutex
	file   StoreFile
	client *cursor_account_sdk.Client
	Log    *slog.Logger
}

// NewStore loads path if it exists (or starts as empty {"sessions":[]}),
// then always writes via the normal tmp+rename save so permission errors
// like "write auth store tmp: ..." surface at startup, not on first mutation.
func NewStore(path string, client *cursor_account_sdk.Client) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("auth store path is empty")
	}
	if client == nil {
		client = &cursor_account_sdk.Client{}
	}
	s := &Store{path: path, client: client}
	if _, err := s.load(); err != nil {
		return nil, err
	}
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) log() *slog.Logger {
	if s != nil && s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// load reads the auth store. created is true when the file was missing.
func (s *Store) load() (created bool, err error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.file = StoreFile{Sessions: []SessionRecord{}}
			return true, nil
		}
		return false, fmt.Errorf("read auth store: %w", err)
	}
	if len(data) == 0 {
		s.file = StoreFile{Sessions: []SessionRecord{}}
		return false, nil
	}
	var parsed StoreFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return false, fmt.Errorf("parse auth store: %w", err)
	}
	if parsed.Sessions == nil {
		parsed.Sessions = []SessionRecord{}
	}
	s.file = parsed
	return false, nil
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("mkdir auth store: %w", err)
	}
	data, err := json.MarshalIndent(s.file, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write auth store tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename auth store: %w", err)
	}
	return nil
}

// List returns a copy of all sessions as accounts.
func (s *Store) List() []*cursor_account_sdk.Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*cursor_account_sdk.Account, 0, len(s.file.Sessions))
	for _, r := range s.file.Sessions {
		out = append(out, accountFromRecord(r))
	}
	return out
}

// Get returns one session by id.
func (s *Store) Get(id string) (*cursor_account_sdk.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.file.Sessions {
		if r.ID == id {
			return accountFromRecord(r), nil
		}
	}
	return nil, ErrNotFound
}

// Add persists a new account session.
func (s *Store) Add(account *cursor_account_sdk.Account) error {
	if account == nil || account.ID == "" {
		return fmt.Errorf("account is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.file.Sessions {
		if r.ID == account.ID {
			return fmt.Errorf("session %q already exists", account.ID)
		}
	}
	s.file.Sessions = append(s.file.Sessions, recordFromAccount(account))
	return s.saveLocked()
}

// UpsertBySubject replaces an existing session with the same JWT subject, or adds.
func (s *Store) UpsertBySubject(account *cursor_account_sdk.Account) (merged bool, err error) {
	if account == nil {
		return false, fmt.Errorf("account is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if account.Subject != "" {
		for i, r := range s.file.Sessions {
			if r.Subject != "" && r.Subject == account.Subject {
				account.ID = r.ID
				if account.CreatedAt == 0 {
					account.CreatedAt = r.CreatedAt
				}
				// Keep a known tier when the incoming account still has a placeholder.
				if !cursor_account_sdk.TierKnown(account.Tier) && cursor_account_sdk.TierKnown(r.Tier) {
					account.Tier = r.Tier
				}
				s.file.Sessions[i] = recordFromAccount(account)
				return true, s.saveLocked()
			}
		}
	}
	if account.ID == "" {
		return false, fmt.Errorf("account id is required")
	}
	s.file.Sessions = append(s.file.Sessions, recordFromAccount(account))
	return false, s.saveLocked()
}

// Update writes an existing session back to disk.
func (s *Store) Update(account *cursor_account_sdk.Account) error {
	if account == nil || account.ID == "" {
		return fmt.Errorf("account is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.file.Sessions {
		if r.ID == account.ID {
			s.file.Sessions[i] = recordFromAccount(account)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// Remove deletes one session by id. If id is empty, clears all sessions.
func (s *Store) Remove(id string) (removed int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		removed = len(s.file.Sessions)
		s.file.Sessions = nil
		return removed, s.saveLocked()
	}
	kept := s.file.Sessions[:0]
	for _, r := range s.file.Sessions {
		if r.ID == id {
			removed++
			continue
		}
		kept = append(kept, r)
	}
	if removed == 0 {
		return 0, ErrNotFound
	}
	s.file.Sessions = kept
	return removed, s.saveLocked()
}

// Find returns one session by store id or JWT subject.
func (s *Store) Find(id string) (*cursor_account_sdk.Account, error) {
	if id == "" {
		return nil, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.file.Sessions {
		if r.ID == id {
			return accountFromRecord(r), nil
		}
	}
	for _, r := range s.file.Sessions {
		if r.Subject != "" && r.Subject == id {
			return accountFromRecord(r), nil
		}
	}
	return nil, ErrNotFound
}

// RemoveMatch deletes the session whose store id or JWT subject equals id.
func (s *Store) RemoveMatch(id string) (removed int, err error) {
	acc, err := s.Find(id)
	if err != nil {
		return 0, err
	}
	return s.Remove(acc.ID)
}

// ParseAuthPayload accepts Cursor-style token JSON (access/refresh variants).
func ParseAuthPayload(data []byte) (cursor_account_sdk.Credentials, error) {
	return parseImportCredentials(data)
}

// TestAndStore refreshes creds against Cursor, then upserts by subject.
func (s *Store) TestAndStore(ctx context.Context, creds cursor_account_sdk.Credentials) (*cursor_account_sdk.Account, bool, error) {
	if creds.Refresh == "" {
		return nil, false, fmt.Errorf("%w: missing refresh token", ErrInvalidImport)
	}
	refreshed, err := s.client.RefreshToken(ctx, creds.Refresh)
	if err != nil {
		return nil, false, err
	}
	account, err := cursor_account_sdk.NewAccountFromCredentials(refreshed, time.Now())
	if err != nil {
		return nil, false, err
	}
	s.enrichTier(ctx, account)
	merged, err := s.UpsertBySubject(account)
	if err != nil {
		return nil, false, err
	}
	return account, merged, nil
}

// PublicAccountID is the Control API account id: JWT subject when present, else store id.
func PublicAccountID(a *cursor_account_sdk.Account) string {
	if a == nil {
		return ""
	}
	if a.Subject != "" {
		return a.Subject
	}
	return a.ID
}

// EnsureAccess refreshes the account if needed and persists the result.
func (s *Store) EnsureAccess(ctx context.Context, id string) (*cursor_account_sdk.Account, error) {
	return s.ensureAccess(ctx, id, false)
}

// CheckAccess always validates against Cursor by refreshing, then persists.
func (s *Store) CheckAccess(ctx context.Context, id string) (*cursor_account_sdk.Account, error) {
	return s.ensureAccess(ctx, id, true)
}

func (s *Store) ensureAccess(ctx context.Context, id string, forceRefresh bool) (*cursor_account_sdk.Account, error) {
	s.mu.Lock()
	var acc *cursor_account_sdk.Account
	for _, r := range s.file.Sessions {
		if r.ID == id {
			acc = accountFromRecord(r)
			break
		}
	}
	s.mu.Unlock()
	if acc == nil {
		return nil, ErrNotFound
	}

	now := time.Now()
	refreshed := false
	if forceRefresh || acc.NeedsRefresh(now) {
		creds, err := s.client.RefreshToken(ctx, acc.Refresh)
		if err != nil {
			return nil, err
		}
		acc.ApplyCredentials(creds, now)
		refreshed = true
	}

	prevTier := acc.Tier
	if refreshed || !cursor_account_sdk.TierKnown(acc.Tier) {
		s.enrichTier(ctx, acc)
	}
	if refreshed || acc.Tier != prevTier {
		if err := s.Update(acc); err != nil {
			return nil, err
		}
	}
	return acc, nil
}

// LoginInteractive runs PKCE login and stores the new session (merge by subject).
func (s *Store) LoginInteractive(ctx context.Context) (*cursor_account_sdk.Account, error) {
	creds, _, err := s.client.Login(ctx, cursor_account_sdk.DefaultOnLoginURL)
	if err != nil {
		return nil, err
	}
	account, err := cursor_account_sdk.NewAccountFromCredentials(creds, time.Now())
	if err != nil {
		return nil, err
	}
	s.enrichTier(ctx, account)
	if _, err := s.UpsertBySubject(account); err != nil {
		return nil, err
	}
	return account, nil
}

// cursorAuthFile shapes accepted by ImportAuthFile.
type cursorAuthFile struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	Access       string `json:"access"`
	Refresh      string `json:"refresh"`
	Expires      int64  `json:"expires"`
	Type         string `json:"type"`
}

// ImportAuthFile reads a Cursor/OpenCode-style auth JSON and merges into the store.
// Supports:
//   - { "accessToken", "refreshToken" }
//   - { "access", "refresh", "expires"? }
//   - { "cursor": { ... one of the above ... } }
//   - { "type":"oauth", "access", "refresh", "expires" }
func (s *Store) ImportAuthFile(path string) (*cursor_account_sdk.Account, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read import file: %w", err)
	}
	creds, err := parseImportCredentials(data)
	if err != nil {
		return nil, err
	}
	account, err := cursor_account_sdk.NewAccountFromCredentials(creds, time.Now())
	if err != nil {
		return nil, err
	}
	s.enrichTier(context.Background(), account)
	if _, err := s.UpsertBySubject(account); err != nil {
		return nil, err
	}
	return account, nil
}

// enrichTier best-effort fills Account.Tier from Cursor stripe profile.
// Failures leave the existing tier unchanged.
func (s *Store) enrichTier(ctx context.Context, account *cursor_account_sdk.Account) {
	if s == nil || account == nil || account.Access == "" {
		return
	}
	id := PublicAccountID(account)
	tier, err := s.client.FetchTier(ctx, account.Access)
	if err != nil {
		s.log().Warn("tier enrich failed", "session", id, "err", err)
		return
	}
	if !cursor_account_sdk.TierKnown(tier) {
		s.log().Warn("tier enrich empty", "session", id)
		return
	}
	if account.Tier == tier {
		return
	}
	prev := account.Tier
	account.Tier = tier
	account.UpdatedAt = time.Now().UnixMilli()
	s.log().Debug("tier enriched", "session", id, "from", prev, "tier", tier)
}

func parseImportCredentials(data []byte) (cursor_account_sdk.Credentials, error) {
	// Nested provider map (OpenCode auth.json).
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(data, &nested); err == nil {
		if raw, ok := nested["cursor"]; ok {
			return parseImportCredentials(raw)
		}
	}

	var raw cursorAuthFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return cursor_account_sdk.Credentials{}, fmt.Errorf("%w: %v", ErrInvalidImport, err)
	}

	access := raw.Access
	if access == "" {
		access = raw.AccessToken
	}
	refresh := raw.Refresh
	if refresh == "" {
		refresh = raw.RefreshToken
	}
	if refresh == "" {
		return cursor_account_sdk.Credentials{}, fmt.Errorf("%w: missing refresh token", ErrInvalidImport)
	}
	if access == "" {
		// Allow refresh-only import; caller can refresh later.
		access = ""
	}
	expires := raw.Expires
	if expires == 0 && access != "" {
		expires = cursor_account_sdk.TokenExpiryMilli(access, time.Now())
	}
	return cursor_account_sdk.Credentials{
		Access:    access,
		Refresh:   refresh,
		ExpiresAt: expires,
	}, nil
}
