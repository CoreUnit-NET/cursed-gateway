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

It handles Cursor OAuth login, keeps access/refresh tokens on disk, refreshes them on a staggered schedule, load-balances across multiple Cursor accounts (preferring Pro), and maps `/ai/v1/chat/completions` (and related OpenAI-like routes) onto Cursor’s internal Connect/gRPC agent protocol.

The Control API under `/api` exposes the same account store and login attempts as REST resources (accounts, login attempts, service state), separate from the AI surface under `/ai` (example `/ai/v1/models`). `/v1` still works as an alias.

Put it on localhost or a private network next to your agents. Terminate TLS and extra edge auth in front if you need them—this process stays a plain HTTP gateway.

<details><summary><strong>How it works</strong></summary>

### How it works

1. Client calls OpenAI-compatible HTTP (`/ai/v1/models`, `/ai/v1/chat/completions`, stream or non-stream; `/v1` still works). A separate Control API under `/api` manages accounts, login attempts, and service state on the same process — it is not on the completion path.
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
- **Account management CLI:** `logout` and `sessions` operate on the session store / config only—no need for a running gateway process.
- **Control API:** REST under `/api` for accounts, login attempts, and service state — resource/state URLs, not CLI task names. Add accounts with `POST /api/accounts` (test against Cursor, then store) or by finishing a `/api/login` attempt.
- **HTTP path split:** AI under `/ai` (example `/ai/v1/models`); Control API under `/api`. `/v1` remains as an alias.
- **Staggered token refresh:** Spreads refresh work across accounts (`lifetime - margin`, oldest refresh first). Boot fast-refresh handles tokens close to expiry first.
- **OpenAI-compatible API:** Text, chat, and streaming; image/media where Cursor supports it.
- **Model discovery:** Exposes Cursor models via `/ai/v1/models` (`/v1/models` still works) and a `models` CLI command.
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

When `serve` is running, the [Control API](#control-api) exposes the same store as REST under `/api` (accounts, login attempts, and service state). CLI task names are not HTTP paths. The gateway is a shared account pool and has no mapping from caller to account.

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

HTTP: `POST /api/login` creates a PKCE attempt and returns `id` plus `url`. Completing that attempt in the browser adds the account. See [Control API](#control-api).

### import

Import a Cursor-style `auth.json` into the gateway session store (`AUTH_PATH`). This is the only supported way to consume Cursor `auth.json`:

```sh
cursed-gateway import
cursed-gateway import ./path/to/auth.json
```

Default import source is `./data/auth.json` when no path is given. Existing sessions in the gateway store are merged, not replaced wholesale.

HTTP: `POST /api/accounts` with Cursor token JSON (`refreshToken` or `refresh`) tests against Cursor, then stores. `import` stays the CLI path for an `auth.json` file. See [Control API](#control-api).

### logout

Remove one or more sessions from the gateway session store (file/config only):

```sh
cursed-gateway logout
cursed-gateway logout <id>
```

`<id>` is the same public account id as `GET /api/accounts` (JWT subject, or store UUID if subject is empty). HTTP: `DELETE /api/accounts/<id>` deletes the local session. See [Control API](#control-api).

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

HTTP: `GET /api/accounts` lists pool accounts (`id`, `subject`, `tier`, `expires`), never access/refresh tokens. That is store metadata, not who owns the account. `--check` stays a one-shot CLI action. See [Control API](#control-api).

### models

Fetch and print models available to the configured Cursor account(s):

```sh
cursed-gateway models
```

HTTP: `/ai/v1/models` (`/v1/models` still works).

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

Point clients at `http://<host>:<port>/ai/v1` (default `http://0.0.0.0:8080/ai/v1`). `/v1` still works as an alias. Control API on the same listener: `GET /api`.

### Control API

HTTP is split by resource, not by CLI task names (`sessions`, `login`):

- `/ai/*` — OpenAI-like AI. Example: `/ai/v1/models`. `/v1` remains as an alias.
- `/api/*` — Control API: accounts, login attempts, and service state.

The process is a shared account pool: callers add accounts and use `/ai`. There is no mapping from caller to account.

Account `id` is the JWT subject from Cursor login data (fallback: store UUID). Login-attempt `id` is Cursor’s PKCE uuid and is never reused as the account `id`. Account payloads are local store fields (`id`, `subject`, `tier`, `expires`); they never include access/refresh tokens.

Errors on most routes are `{"error":"<message>"}`. `POST /api/accounts` uses `{ok, id}` / `{ok:false, error}` instead.

**Service**

`GET /api` — account count, login-attempt count, and the login-attempt limits:

```json
{
  "accounts": 0,
  "login_attempts": 0,
  "max_login_attempts": 3,
  "login_attempt_mins": 3,
  "login_keep_mins": 5
}
```

**Accounts**

- `GET /api/accounts` — list all accounts.
- `GET /api/accounts/<id>` — one account.
- `DELETE /api/accounts/<id>` — delete the local session (`204` empty body).
- `POST /api/accounts` — test the request payload against Cursor, then store. `201` for a new account, `200` if an existing subject was merged.

List:

```json
{
  "accounts": [
    {
      "id": "user_abc",
      "subject": "user_abc",
      "tier": "pro",
      "expires": 1735689600000
    }
  ]
}
```

One account is that object without the wrapper. `expires` is Unix milliseconds.

`POST /api/accounts` body is Cursor token JSON. `refreshToken` or `refresh` is required; `accessToken` / `access` is optional. Nested `{"cursor": { ... }}` is accepted:

```json
{ "refreshToken": "<token>" }
```

Success / failure:

```json
{ "ok": true, "id": "user_abc" }
```

```json
{ "ok": false, "error": "missing refresh token" }
```

**Login attempts**

- `POST /api/login` — create a PKCE attempt (not an account). `201` with `id`, `url`, and `state`. `409` when open attempts are at the cap.
- `GET /api/login` — list open attempts (and recently resolved ones still in the keep window).
- `GET /api/login/<id>` — one attempt: URL, login state, and `account_id` after success.
- `DELETE /api/login/<id>` — close that attempt (`204`).

`POST /api/login` needs no body:

```json
{
  "id": "3fa85f64-5717-4562-b3fc-2c963f66afa6",
  "url": "https://cursor.com/loginDeepControl?challenge=...&uuid=...&mode=login",
  "state": "pending"
}
```

List:

```json
{ "login": [ { "id": "...", "url": "...", "state": "pending" } ] }
```

`state` is `pending`, `succeeded`, `failed`, or `expired`. After success the attempt also has `account_id` (the new account `id`, not the attempt `id`). Failed attempts may include `error`.

Two ways to add an account: `POST /api/accounts` (above), or complete a login attempt. Success creates the account; the attempt stays listed for `LOGIN_KEEP_MINS` (default 5 minutes), then is removed.

Open attempts are capped by `MAX_LOGIN_ATTEMPTS` (default 3) and closed after `LOGIN_ATTEMPT_MINS` (default 3 minutes) if unanswered. Those limits are environment variables only.

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

Login-attempt limits are environment variables only (no flags):

- `MAX_LOGIN_ATTEMPTS`: max open Control API login attempts, defaults to `3`
- `LOGIN_ATTEMPT_MINS`: close unanswered login attempts after this many minutes, defaults to `3`
- `LOGIN_KEEP_MINS`: keep a resolved login attempt after the account is created, defaults to `5`

Logging always uses `log/slog` text on stderr. There is no `LOG_FORMAT` switch.

Treat `AUTH_PATH` as secret. Do not commit it.

Proto toolchain flags live under [Proto Pipeline](#proto-pipeline).

### Proto Pipeline

Auxiliary toolchain under `cmd/proto` (not the gateway binary). Regenerates
`lib/cursorProto` from a local Cursor agent; used only for development /
codegen—not by `serve`. Run via `make proto` / `go run ./cmd/proto`.

Flags and environment variables:

- `PROTO_CACHE_DIR` or `--cache-dir`: local cache for tools and descriptor artifacts, defaults to `./tmp/proto`
- `PROTO_OUT` or `--proto-out`: generated Go protobuf output directory, defaults to `./lib/cursorProto`
- `PROTO_AGENT_BIN` or `--agent-bin`: local cursor-agent path (versions dir, share root, or single file)
- `--force`: ignore input fingerprint cache and regenerate

### Reverse proxy examples

Terminate TLS and optionally serve a static Control UI in front of the plain HTTP gateway.

Caddy:

```caddyfile
cursed-gateway.example.com:443 {
	# no auth, you need to somehow block unwanted request or run locally!

	handle /ai* {
		reverse_proxy http://cursed-gateway-host:8080
	}

	handle /api* {
		reverse_proxy http://cursed-gateway-host:8080
	}

	handle {
		root * /path/to/ui
		file_server
	}
}
```

nginx equivalent:

```nginx
server {
	listen 443 ssl;
	server_name cursed-gateway.example.com;

	# no auth, you need to somehow block unwanted request or run locally!

	location /ai {
		proxy_pass http://cursed-gateway-host:8080;
	}

	location /api {
		proxy_pass http://cursed-gateway-host:8080;
	}

	location / {
		root /path/to/ui;
	}
}
```

</details>

<details><summary><strong>User Guide</strong></summary>

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

</details>

<details><summary><strong>Development</strong></summary>

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

</details>

<div align="center">

# 🤝 Contributing

Contributions to this project are welcome!  
Follow the [CONTRIBUTING.md](CONTRIBUTING.md) for more infos.

# ⚠️ Disclaimer

This project is provided without warranties.

# 📜 License

Licensed under the [GNU Affero General Public License v3](LICENSE).

<a href="https://discord.coreunit.net">
    <img alt="CoreUnit.NET Discord Banner" src="https://discord.com/api/guilds/422136748294930443/widget.png?style=banner2">
</a>

</div>