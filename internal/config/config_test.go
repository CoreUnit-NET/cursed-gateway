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
		"PREFER_PRO", "VERBOSE", "ENABLE_LOGIN",
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
	if cfg.AuthPath != "./data/data.json" {
		t.Fatalf("AuthPath = %q, want ./data/data.json", cfg.AuthPath)
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
	if cfg.Verbose {
		t.Fatal("expected Verbose false by default")
	}
	if cfg.EnableLogin {
		t.Fatal("expected EnableLogin false by default")
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
	t.Setenv("VERBOSE", "false")
	t.Setenv("ENABLE_LOGIN", "false")

	os.Args = []string{
		"cursed-gateway", "serve",
		"--host", "10.0.0.1",
		"-p", "8081",
		"-a", "./from-flag.json",
		"-r", "3",
		"-c", "20",
		"--prefer-pro=true",
		"-b",
		"--enable-login=true",
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
	if !cfg.Verbose {
		t.Fatal("expected Verbose true from flag")
	}
	if !cfg.EnableLogin {
		t.Fatal("expected EnableLogin true from flag")
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
	t.Setenv("VERBOSE", "true")
	t.Setenv("ENABLE_LOGIN", "true")

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
	if !cfg.Verbose {
		t.Fatal("expected Verbose true from env")
	}
	if !cfg.EnableLogin {
		t.Fatal("expected EnableLogin true from env")
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

func TestParseConfigSubcommands(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	clearConfigEnv(t)

	cases := []struct {
		args    []string
		command string
		check   bool
		imp     string
	}{
		{[]string{"cursed-gateway"}, CommandServe, false, "./data/auth.json"},
		{[]string{"cursed-gateway", "login"}, CommandLogin, false, "./data/auth.json"},
		{[]string{"cursed-gateway", "sessions", "--check"}, CommandSessions, true, "./data/auth.json"},
		{[]string{"cursed-gateway", "import", "/tmp/auth.json"}, CommandImport, false, "/tmp/auth.json"},
		{[]string{"cursed-gateway", "serve"}, CommandServe, false, "./data/auth.json"},
	}
	for _, tc := range cases {
		os.Args = tc.args
		cfg, err := ParseConfig("Demo", "demo")
		if err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if cfg.Command != tc.command {
			t.Fatalf("%v: Command=%q want %q", tc.args, cfg.Command, tc.command)
		}
		if cfg.SessionsCheck != tc.check {
			t.Fatalf("%v: SessionsCheck=%v want %v", tc.args, cfg.SessionsCheck, tc.check)
		}
		if cfg.ImportPath != tc.imp {
			t.Fatalf("%v: ImportPath=%q want %q", tc.args, cfg.ImportPath, tc.imp)
		}
	}
}
