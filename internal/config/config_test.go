package config

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"HOST", "PORT", "AUTH_PATH", "MAX_RETRIES", "COOLDOWN_MINS",
		"PREFER_PRO", "LOG_LEVEL", "LOG_FORMAT",
		"CACHE_DIR", "PROTO_OUT", "RELEASE_CHANNEL",
	} {
		t.Setenv(key, "")
	}
}

func TestParseConfigDefaults(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	clearConfigEnv(t)

	os.Args = []string{"cursed-gateway", "serve"}
	cfg, err := ParseConfig("Demo", "demo")
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}

	if cfg.Host != "0.0.0.0" {
		t.Fatalf("Host = %q, want 0.0.0.0", cfg.Host)
	}
	if cfg.Port != 8080 {
		t.Fatalf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.AuthPath != "./auth.json" {
		t.Fatalf("AuthPath = %q, want ./auth.json", cfg.AuthPath)
	}
	if cfg.MaxRetries != 5 {
		t.Fatalf("MaxRetries = %d, want 5", cfg.MaxRetries)
	}
	if cfg.CooldownMins != 15 {
		t.Fatalf("CooldownMins = %d, want 15", cfg.CooldownMins)
	}
	if !cfg.PreferPro {
		t.Fatal("expected PreferPro true by default")
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.LogFormat != "text" {
		t.Fatalf("LogFormat = %q, want text", cfg.LogFormat)
	}
	if cfg.CacheDir != "./.cache" {
		t.Fatalf("CacheDir = %q, want ./.cache", cfg.CacheDir)
	}
	if cfg.ProtoOut != "./pkg/generated" {
		t.Fatalf("ProtoOut = %q, want ./pkg/generated", cfg.ProtoOut)
	}
	if cfg.ReleaseChannel != "prod" {
		t.Fatalf("ReleaseChannel = %q, want prod", cfg.ReleaseChannel)
	}
}

func TestParseConfigFlagsOverrideEnv(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	clearConfigEnv(t)

	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("PORT", "9090")
	t.Setenv("AUTH_PATH", "./from-env.json")
	t.Setenv("MAX_RETRIES", "9")
	t.Setenv("COOLDOWN_MINS", "30")
	t.Setenv("PREFER_PRO", "false")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("CACHE_DIR", "./env-cache")
	t.Setenv("PROTO_OUT", "./env-proto")
	t.Setenv("RELEASE_CHANNEL", "staging")

	os.Args = []string{
		"cursed-gateway", "serve",
		"--host", "10.0.0.1",
		"-p", "8081",
		"-a", "./from-flag.json",
		"-r", "3",
		"-c", "20",
		"--prefer-pro=true",
		"-l", "warn",
		"--log-format", "text",
		"--cache-dir", "./flag-cache",
		"--proto-out", "./flag-proto",
		"--channel", "rc",
	}
	cfg, err := ParseConfig("Demo", "demo")
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}

	if cfg.Host != "10.0.0.1" {
		t.Fatalf("Host = %q, want flag to win", cfg.Host)
	}
	if cfg.Port != 8081 {
		t.Fatalf("Port = %d, want flag to win", cfg.Port)
	}
	if cfg.AuthPath != "./from-flag.json" {
		t.Fatalf("AuthPath = %q, want flag to win", cfg.AuthPath)
	}
	if cfg.MaxRetries != 3 {
		t.Fatalf("MaxRetries = %d, want flag to win", cfg.MaxRetries)
	}
	if cfg.CooldownMins != 20 {
		t.Fatalf("CooldownMins = %d, want flag to win", cfg.CooldownMins)
	}
	if !cfg.PreferPro {
		t.Fatal("expected PreferPro true from flag")
	}
	if cfg.LogLevel != "warn" {
		t.Fatalf("LogLevel = %q, want flag to win", cfg.LogLevel)
	}
	if cfg.LogFormat != "text" {
		t.Fatalf("LogFormat = %q, want flag to win", cfg.LogFormat)
	}
	if cfg.CacheDir != "./flag-cache" {
		t.Fatalf("CacheDir = %q, want flag to win", cfg.CacheDir)
	}
	if cfg.ProtoOut != "./flag-proto" {
		t.Fatalf("ProtoOut = %q, want flag to win", cfg.ProtoOut)
	}
	if cfg.ReleaseChannel != "rc" {
		t.Fatalf("ReleaseChannel = %q, want flag to win", cfg.ReleaseChannel)
	}
}

func TestParseConfigEnvOnly(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	clearConfigEnv(t)

	t.Setenv("HOST", "192.168.1.1")
	t.Setenv("PORT", "7777")
	t.Setenv("AUTH_PATH", "./data/auth.json")
	t.Setenv("PREFER_PRO", "false")
	t.Setenv("LOG_LEVEL", "error")
	t.Setenv("RELEASE_CHANNEL", "experimental")

	os.Args = []string{"cursed-gateway", "serve"}
	cfg, err := ParseConfig("Demo", "demo")
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}

	if cfg.Host != "192.168.1.1" {
		t.Fatalf("Host = %q, want env", cfg.Host)
	}
	if cfg.Port != 7777 {
		t.Fatalf("Port = %d, want env", cfg.Port)
	}
	if cfg.AuthPath != "./data/auth.json" {
		t.Fatalf("AuthPath = %q, want env", cfg.AuthPath)
	}
	if cfg.PreferPro {
		t.Fatal("expected PreferPro false from env")
	}
	if cfg.LogLevel != "error" {
		t.Fatalf("LogLevel = %q, want env", cfg.LogLevel)
	}
	if cfg.ReleaseChannel != "experimental" {
		t.Fatalf("ReleaseChannel = %q, want env", cfg.ReleaseChannel)
	}
}

func TestParseConfigInvalidPort(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	clearConfigEnv(t)

	t.Setenv("PORT", "nope")
	os.Args = []string{"cursed-gateway", "serve"}
	_, err := ParseConfig("Demo", "demo")
	if err == nil {
		t.Fatal("expected error for invalid PORT")
	}
	if !strings.Contains(err.Error(), "PORT") {
		t.Fatalf("expected PORT in error, got: %v", err)
	}
}

func TestParseConfigVersionFlag(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	clearConfigEnv(t)

	os.Args = []string{"cursed-gateway", "--version"}
	cfg, err := ParseConfig("Demo", "demo")
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if !cfg.ShowVersion {
		t.Fatal("expected ShowVersion true")
	}
}

func TestParseConfigHelpRequested(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	clearConfigEnv(t)

	os.Args = []string{"cursed-gateway", "--help"}
	_, err := ParseConfig("Demo", "demo")
	if !errors.Is(err, ErrHelpRequested) {
		t.Fatalf("err = %v, want ErrHelpRequested", err)
	}
}
