package main

/*
Proto toolchain: local cursor-agent → protodump → protoc → lib/cursorProto.

Skips extract/codegen when the cached agent scan binaries match the local
install. Cache/tools live under .tmp/proto; generated Go goes to PROTO_OUT.
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
	fmt.Printf("Done — generated protos under %s\n", cfg.ProtoOut)
}
