package settings

import (
	"testing"

	"github.com/CoreUnit-NET/cursed-gateway/internal/config"
)

func TestFromAppConfigOK(t *testing.T) {
	cfg := &config.AppConfig{
		Command:      config.CommandServe,
		Host:         "127.0.0.1",
		Port:         8080,
		AuthPath:     "./data/data.json",
		MaxRetries:   5,
		CooldownMins: 15,
		PreferPro:    true,
		Verbose:      true,
		ImportPath:   "./data/auth.json",
	}
	s, err := FromAppConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Verbose || !s.PreferPro {
		t.Fatalf("normalization failed: %+v", s)
	}
}

func TestFromAppConfigInvalidPort(t *testing.T) {
	cfg := &config.AppConfig{
		Host:     "127.0.0.1",
		Port:     0,
		AuthPath: "./data/data.json",
	}
	if _, err := FromAppConfig(cfg); err == nil {
		t.Fatal("expected port error")
	}
}
