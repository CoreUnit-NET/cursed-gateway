<div align="center">

# 🌌 cursed-gateway

![AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)
![CI/CD](https://github.com/CoreUnit-NET/cursed-gateway/actions/workflows/go-bin-release.yml/badge.svg)
![CI/CD](https://github.com/CoreUnit-NET/cursed-gateway/actions/workflows/go-test-build.yml/badge.svg)  
![](https://img.shields.io/badge/dynamic/json?color=green&label=watchers&query=watchers&suffix=x&url=https%3A%2F%2Fapi.github.com%2Frepos%2FCoreUnit-NET%2Fcursed-gateway)
![](https://img.shields.io/badge/dynamic/json?color=yellow&label=stars&query=stargazers_count&suffix=x&url=https%3A%2F%2Fapi.github.com%2Frepos%2FCoreUnit-NET%2Fcursed-gateway)
![](https://img.shields.io/badge/dynamic/json?color=navy&label=forks&query=forks&suffix=x&url=https%3A%2F%2Fapi.github.com%2Frepos%2FCoreUnit-NET%2Fcursed-gateway)

</div>

_Do you want to use your own account for different personal AI use cases?_
`cursed-gateway` is a Cursor API proxy gateway that solves that for you.

## About

`cursed-gateway` is a Go proxy for setups where several clients need Cursor models through a normal OpenAI-shaped HTTP API.

It handles Cursor OAuth login, keeps access/refresh tokens on disk, refreshes them on a staggered schedule, load-balances across multiple Cursor accounts (preferring Pro), and maps `/v1/chat/completions` (and related OpenAI-like routes) onto Cursor’s internal Connect/gRPC agent protocol.

Put it on localhost or a private network next to your agents. Terminate TLS and extra edge auth in front if you need them—this process stays a plain HTTP gateway.

<details><summary><strong>How it works</strong></summary>

### How it works

1. Client calls OpenAI-compatible HTTP (`/v1/models`, `/v1/chat/completions`, stream or non-stream).
2. Gateway buffers the request and picks a healthy account from the pool.
3. Upstream talk goes to Cursor over HTTP/2 Connect (`api2.cursor.sh` by default).
4. Response headers/status stay held until upstream init succeeds; on early account errors the gateway retries with the next account before committing the client response.
5. Background refresh keeps tokens alive: a fast queue on boot for near-expiry tokens, then a spaced refresh loop across accounts.

</details>

<details><summary><strong>Features</strong></summary>

### Features

- **OAuth login CLI:** `login` runs a Cursor PKCE login flow and stores tokens locally in the gateway session store.
- **Cursor auth.json import:** `import` is the only path that reads Cursor-style `auth.json` and merges sessions into the gateway store.
- **Multi-account store:** Run several Cursor accounts at once under one gateway.
- **Account management CLI:** `logout`, `sessions`, and `whoami` operate on the session store / config only—no need for a running gateway process.
- **Staggered token refresh:** Spreads refresh work across accounts (`lifetime - margin`, oldest refresh first). Boot fast-refresh handles tokens close to expiry first.
- **OpenAI-compatible API:** Text, chat, and streaming; image/media where Cursor supports it.
- **Model discovery:** Exposes Cursor models via `/v1/models` and a `models` CLI command.
- **Account load balancing:** Rotates healthy accounts, prefers Pro over Free, cools down rate-limited accounts.
- **Delayed-header fallback:** Buffers the client body and withholds headers until upstream init succeeds; fails over to the next account on pre-stream errors.
- **Cursor rate-limit awareness:** Treats upstream 429 / equivalent limits as pool cooldown signals.
- **Proto Pipeline:** Dockerized Cursor-agent download, protobuf extract, Go codegen, and mtime-based cache.

</details>

<details><summary><strong>Out of scope</strong></summary>

### Out of scope

- **Client request rate limiting:** Not implemented; use a reverse proxy if you need it.
- **Request auth / API keys:** The gateway does not gate callers with keys or tokens.
- **HTTPS / TLS termination:** Listen plain HTTP; terminate TLS in front (Caddy, etc.).

</details>

<details><summary><strong>Usage</strong></summary>

## Usage

Account and inspect commands read/write `AUTH_PATH` (the gateway session store) and related config. They do **not** talk to a running `serve` process.

Cursor’s own `auth.json` is **not** used as the live store. Bring those sessions in only with [`import`](#import).

Show help messages:

```sh
cursed-gateway
```

### login

Start the Cursor OAuth PKCE flow and write the access/refresh session into the gateway session store:

```sh
cursed-gateway login
```

### import

Import a Cursor-style `auth.json` into the gateway session store (`AUTH_PATH`). This is the only supported way to consume Cursor `auth.json`:

```sh
cursed-gateway import
cursed-gateway import ./path/to/auth.json
```

Default import source is `./data/auth.json` when no path is given. Existing sessions in the gateway store are merged, not replaced wholesale.

### logout

Remove one or more sessions from the gateway session store (file/config only):

```sh
cursed-gateway logout
cursed-gateway logout <session-id>
```

### sessions

List stored account sessions (access/refresh token sessions):

```sh
cursed-gateway sessions
```

Validate sessions against Cursor (still a one-shot CLI action, not tied to a running gateway):

```sh
cursed-gateway sessions --check
```

Each checked session prints a status such as `valid`, `invalid`, or `error: <message>`.

### whoami

Show which sessions/accounts are in the store and basic identity metadata from local state:

```sh
cursed-gateway whoami
```

### models

Fetch and print models available to the configured Cursor account(s):

```sh
cursed-gateway models
```

### version

Print build version information:

```sh
cursed-gateway version
# or
cursed-gateway -v
```

### serve

Start the OpenAI-compatible proxy:

```sh
cursed-gateway serve
```

Point clients at `http://<host>:<port>/v1` (default `http://0.0.0.0:8080/v1`).

</details>

<details><summary><strong>Configuration</strong></summary>

## Configuration

CLI flags and environment variables can both be used. **Flags override env values.**

A `.env` file in the working directory is loaded at startup when present (missing file is ignored).

`serve` / runtime flags and environment variables:

- `HOST` or `--host`: bind host, defaults to `0.0.0.0`
- `PORT` or `-p` / `--port`: bind port, defaults to `8080`
- `AUTH_PATH` or `-a` / `--auth`: gateway multi-account session store (not Cursor `auth.json`), defaults to `./data/data.json`
- `MAX_RETRIES` or `-r` / `--retries`: max account fallback attempts per request, defaults to `5`
- `COOLDOWN_MINS` or `-c` / `--cooldown`: cooldown minutes for rate-limited accounts, defaults to `15`
- `PREFER_PRO` or `--prefer-pro`: prefer Pro accounts over Free, defaults to `true`
- `VERBOSE` or `-b` / `--verbose`: enable debug and trace logs, defaults to `false`
- `ENABLE_LOGIN` or `--enable-login`: expose `GET /login` as a 307 redirect to Cursor OAuth, defaults to `false`

Logging always uses `log/slog` text on stderr. There is no `LOG_FORMAT` switch.

Treat `AUTH_PATH` as secret. Do not commit it.

Proto toolchain flags live under [Proto Pipeline](#proto-pipeline).

### Proto Pipeline

Auxiliary toolchain under `cmd/proto` (not the gateway binary). Regenerates
`lib/cursorProto` from a local Cursor agent; used only for development /
codegen—not by `serve`. Run via `make proto` / `go run ./cmd/proto`.

Flags and environment variables:

- `PROTO_CACHE_DIR` or `--cache-dir`: local cache for tools and descriptor artifacts, defaults to `./.tmp/proto`
- `PROTO_OUT` or `--proto-out`: generated Go protobuf output directory, defaults to `./lib/cursorProto`
- `PROTO_AGENT_BIN` or `--agent-bin`: local cursor-agent path (versions dir, share root, or single file)
- `--force`: ignore input fingerprint cache and regenerate

</details>

# User Guide

## Requirements

Linux- or macos-like systems with `go` or `wget & tar` installed.

## Getting Started

Start the latest repo version directly without leaving stuff in the current working dir:

```sh
go run github.com/CoreUnit-NET/cursed-gateway@latest
```

## Quick help

```sh
go run github.com/CoreUnit-NET/cursed-gateway@latest -h
```

## Install via go

###### _For this section go is required, check out the [install go guide](#install-go)._

```sh
go install github.com/CoreUnit-NET/cursed-gateway@latest
```

## Install via wget

```sh
export CUSTOM_BIN_DIR="/usr/local/bin" # <- change if needed
export CUSTOM_VERSION="" # <- set latest version here

rm -rf $CUSTOM_BIN_DIR/cursed-gateway
wget https://github.com/CoreUnit-NET/cursed-gateway/releases/download/v$CUSTOM_VERSION/cursed-gateway-v$CUSTOM_VERSION-linux-amd64.tar.gz -O /tmp/cursed-gateway.tar.gz
tar -xzvf /tmp/cursed-gateway.tar.gz -C $CUSTOM_BIN_DIR/ cursed-gateway
rm /tmp/cursed-gateway.tar.gz
```

# Build

## Build requirements

To build, you need to install go.
The required go version is in the `go.mod` file.

## Build Instructions

###### _For this section go is required, check out the [install go guide](#install-go)._

Clone the repo:

```sh
git clone https://github.com/CoreUnit-NET/cursed-gateway.git
cd cursed-gateway
```

Build the cursed-gateway binary from source code:

```sh
make build
./cursed-gateway
```

# Development

###### _For this section go is required, check out the [install go guide](#install-go)._

This part is work in progress, I want to use 'AIR' as auto-reload tool:

```sh
make dev #WIP
```

## Install go

The required go version for this project is in the `go.mod` file.

To install and update go, I can recommend the following repo:

```sh
git clone git@github.com:udhos/update-golang.git golang-updater
cd golang-updater
sudo ./update-golang.sh
```

# Contributing

Contributions to this project are welcome!  
Interested users can refer to the guidelines provided in the [CONTRIBUTING.md](CONTRIBUTING.md) file to contribute to the project and help improve its functionality and features.

# License

This project is licensed under the [MIT license](LICENSE), providing users with flexibility and freedom to use and modify the software according to their needs.

# Disclaimer

This project is provided without warranties.  
Users are advised to review the accompanying license for more information on the terms of use and limitations of liability.