package settings

/*
Package settings turns raw config into a validated, app-ready Settings
struct.

Convert/parse values into usable types (including atomics if needed).
Every field must have a validator that runs before Settings is returned.
Session token persistence stays in login_session; this package only
exposes paths and runtime options from config (e.g. AUTH_PATH / data dir).

Proto toolchain options (PROTO_CACHE_DIR / PROTO_OUT / PROTO_AGENT_BIN) live
only in cmd/proto — not here.
*/

import (
	"fmt"
	"strings"
	"time"

	"github.com/CoreUnit-NET/cursed-gateway/internal/config"
)

type Settings struct {
	Command       string
	Args          []string
	ShowVersion   bool
	SessionsCheck bool
	ImportPath    string

	Host             string
	Port             int
	AuthPath         string
	MaxRetries       int
	CooldownMins     int
	PreferPro        bool
	Verbose          bool
	MaxLoginAttempts int
	LoginAttemptMins int
	LoginKeepMins    int
}

// FromAppConfig validates cfg and returns Settings.
func FromAppConfig(cfg *config.AppConfig) (*Settings, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	s := &Settings{
		Command:          cfg.Command,
		Args:             append([]string(nil), cfg.Args...),
		ShowVersion:      cfg.ShowVersion,
		SessionsCheck:    cfg.SessionsCheck,
		ImportPath:       strings.TrimSpace(cfg.ImportPath),
		Host:             strings.TrimSpace(cfg.Host),
		Port:             cfg.Port,
		AuthPath:         strings.TrimSpace(cfg.AuthPath),
		MaxRetries:       cfg.MaxRetries,
		CooldownMins:     cfg.CooldownMins,
		PreferPro:        cfg.PreferPro,
		Verbose:          cfg.Verbose,
		MaxLoginAttempts: cfg.MaxLoginAttempts,
		LoginAttemptMins: cfg.LoginAttemptMins,
		LoginKeepMins:    cfg.LoginKeepMins,
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Settings) validate() error {
	if s.Host == "" {
		return fmt.Errorf("host must not be empty")
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", s.Port)
	}
	if s.AuthPath == "" {
		return fmt.Errorf("auth path must not be empty")
	}
	if s.MaxRetries < 0 {
		return fmt.Errorf("max retries must be >= 0")
	}
	if s.CooldownMins < 0 {
		return fmt.Errorf("cooldown minutes must be >= 0")
	}
	if s.MaxLoginAttempts < 1 {
		return fmt.Errorf("max login attempts must be >= 1")
	}
	if s.LoginAttemptMins < 1 {
		return fmt.Errorf("login attempt minutes must be >= 1")
	}
	if s.LoginKeepMins < 1 {
		return fmt.Errorf("login keep minutes must be >= 1")
	}
	if s.Command == config.CommandImport && s.ImportPath == "" {
		return fmt.Errorf("import path must not be empty")
	}
	return nil
}

// LoginAttemptTimeout is how long an unanswered Control API login attempt stays open.
func (s *Settings) LoginAttemptTimeout() time.Duration {
	mins := 3
	if s != nil && s.LoginAttemptMins > 0 {
		mins = s.LoginAttemptMins
	}
	return time.Duration(mins) * time.Minute
}

// LoginKeepDuration is how long a resolved login attempt stays listed.
func (s *Settings) LoginKeepDuration() time.Duration {
	mins := 5
	if s != nil && s.LoginKeepMins > 0 {
		mins = s.LoginKeepMins
	}
	return time.Duration(mins) * time.Minute
}
