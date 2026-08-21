import { badge, fillSlot, html, join, mountView, sameView } from "../lib.js";
import { accountHref, loginHref } from "../router.js";
import { sortedLogins, state } from "../state.js";
import { empty } from "./helpers.js";

function compactAccount(account) {
  return html` <a class="row compact" href="${accountHref(account.id)}">
    <div>
      <div class="id">${account.id}</div>
      <div class="meta">
        ${badge(account.tier || "unknown", account.tier || "unknown")}
      </div>
    </div>
  </a>`;
}

function compactLogin(attempt) {
  return html` <a class="row compact" href="${loginHref(attempt.id)}">
    <div>
      <div class="id">${attempt.id}</div>
      <div class="meta">${badge(attempt.state, attempt.state)}</div>
    </div>
  </a>`;
}

export function renderOverview(page, { patch }) {
  const s = state.service;
  if (!patch || !sameView(page, "overview")) {
    mountView(
      page,
      "overview",
      html` <div class="head">
          <div>
            <h2>Overview</h2>
            <p>
              Control API home under <code>/api/*</code>. Manage the account
              pool, login attempts, or try same-origin <code>/ai/*</code>.
            </p>
          </div>
        </div>
        <div class="stats" data-slot="stats"></div>
        <div class="grid-2">
          <div class="card">
            <h3>Recent accounts</h3>
            <div class="body" data-slot="recent-accounts"></div>
          </div>
          <div class="card">
            <h3>Login attempts</h3>
            <div class="body" data-slot="recent-logins"></div>
          </div>
        </div>
        <div class="mode-grid">
          <a class="mode-card" href="#/accounts">
            <em>Accounts · pool</em>
            <strong>Browse sessions</strong>
            <span>List local pool accounts. Tokens are never shown.</span>
          </a>
          <a class="mode-card" href="#/accounts/add">
            <em>Accounts · add</em>
            <strong>Paste tokens</strong>
            <span
              >POST /api/accounts with a refresh token. Cursor is tested
              first.</span
            >
          </a>
          <a class="mode-card" href="#/accounts/import">
            <em>Accounts · import</em>
            <strong>JSON import</strong>
            <span
              >Send Cursor-style JSON, including nested
              <code>cursor</code> objects.</span
            >
          </a>
          <a class="mode-card" href="#/login">
            <em>Login attempts</em>
            <strong>Attempt resources</strong>
            <span
              >GET/POST /api/login-attempts. Create an attempt, then Open or
              Copy its URL.</span
            >
          </a>
          <a class="mode-card" href="#/ai">
            <em>AI · models</em>
            <strong>List catalog</strong>
            <span
              >GET /ai/v1/models. Default Auto is id <code>default</code>.</span
            >
          </a>
          <a class="mode-card" href="#/ai/test">
            <em>AI · test</em>
            <strong>Test a model</strong>
            <span
              >POST /ai/v1/chat/completions or /ai/v1/completions. Pick a model,
              optional SSE, cancel, inspect JSON.</span
            >
          </a>
        </div>`,
    );
  }

  fillSlot(
    page,
    "stats",
    html` <div class="stat">
        <span>Accounts</span><b>${s.accounts ?? "—"}</b>
      </div>
      <div class="stat">
        <span>Login attempts</span><b>${s.login_attempts ?? "—"}</b>
      </div>
      <div class="stat">
        <span>Max open attempts</span><b>${s.max_login_attempts ?? "—"}</b>
      </div>
      <div class="stat">
        <span>Attempt timeout</span><b>${s.login_attempt_mins ?? "—"}m</b>
      </div>
      <div class="stat">
        <span>Keep window</span><b>${s.login_keep_mins ?? "—"}m</b>
      </div>`,
  );

  fillSlot(
    page,
    "recent-accounts",
    state.accounts.length
      ? html`<div class="rows">
          ${join(state.accounts.slice(0, 4), compactAccount)}
        </div>`
      : empty("None yet.", "#/accounts/add", "Add tokens"),
  );

  fillSlot(
    page,
    "recent-logins",
    state.loginAttempts.length
      ? html`<div class="rows">
          ${join(sortedLogins().slice(0, 4), compactLogin)}
        </div>`
      : empty("None yet.", "#/login", "Login attempts"),
  );
}
