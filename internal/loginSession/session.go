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
