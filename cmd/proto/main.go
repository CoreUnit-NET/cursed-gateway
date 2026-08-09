package main

/*
Proto toolchain: local cursor-agent → descriptors → protoc → lib/cursorProto.

Skips work when the agent version and descriptor sources are unchanged.
Tools live under PROTO_CACHE_DIR; generated Go goes to PROTO_OUT.
*/

import (
	"errors"
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "warning: .env: %v\n", err)
	}

	cfg, err := ParseConfig()
	if errors.Is(err, ErrHelpRequested) {
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	deps, err := Ensure(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := Run(cfg, deps); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Done — %s\n", cfg.ProtoOut)
}
