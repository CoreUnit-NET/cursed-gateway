package main

/*
Flags/env for the proto toolchain. Owned only by cmd/proto — not by serve.
*/

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const (
	defaultCacheDir = "./.tmp/proto"
	defaultProtoOut = "./lib/cursorProto"
)

// Config holds proto pipeline options.
type Config struct {
	CacheDir string
	ProtoOut string
	Force    bool
	AgentBin string // package dir, versions dir, share root, or single file
}

func defaultConfig() *Config {
	return &Config{
		CacheDir: defaultCacheDir,
		ProtoOut: defaultProtoOut,
		AgentBin: defaultLocalAgentVersionsDir(),
	}
}

func defaultLocalAgentVersionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", "cursor-agent", "versions")
}

func loadEnv(cfg *Config) {
	if v := strings.TrimSpace(os.Getenv("CACHE_DIR")); v != "" {
		cfg.CacheDir = v
	}
	if v := strings.TrimSpace(os.Getenv("PROTO_OUT")); v != "" {
		cfg.ProtoOut = v
	}
	if v := strings.TrimSpace(os.Getenv("PROTO_AGENT_BIN")); v != "" {
		cfg.AgentBin = v
	}
}

func (cfg *Config) validate() error {
	if cfg.CacheDir == "" {
		return fmt.Errorf("cache dir must not be empty")
	}
	if cfg.ProtoOut == "" {
		return fmt.Errorf("proto out must not be empty")
	}
	if cfg.AgentBin == "" {
		return fmt.Errorf("agent-bin must not be empty (set PROTO_AGENT_BIN or install cursor-agent under ~/.local/share/cursor-agent/versions)")
	}
	return nil
}

// ErrHelpRequested is returned when the user asked for help.
var ErrHelpRequested = errors.New("help requested")

// ParseConfig loads env + flags for the proto toolchain.
func ParseConfig() (*Config, error) {
	cfg := defaultConfig()
	loadEnv(cfg)

	root := &cobra.Command{
		Use:   "proto",
		Short: "Extract Cursor protobufs and generate Go into lib/cursorProto",
		Long:  "Compares local cursor-agent to the cached copy; only extracts/generates when it changed.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	root.Flags().StringVar(&cfg.CacheDir, "cache-dir", cfg.CacheDir, "tool/agent cache (CACHE_DIR)")
	root.Flags().StringVar(&cfg.ProtoOut, "proto-out", cfg.ProtoOut, "generated Go output dir (PROTO_OUT)")
	root.Flags().BoolVar(&cfg.Force, "force", false, "redo even if cached agent binary is unchanged")
	root.Flags().StringVar(&cfg.AgentBin, "agent-bin", cfg.AgentBin, "local agent package or versions dir (PROTO_AGENT_BIN)")

	root.SetHelpCommand(&cobra.Command{Use: "", Hidden: true})

	cmd, err := root.ExecuteC()
	if err != nil {
		return nil, err
	}
	if helpFlag := cmd.Flags().Lookup("help"); helpFlag != nil && helpFlag.Changed {
		return nil, ErrHelpRequested
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
