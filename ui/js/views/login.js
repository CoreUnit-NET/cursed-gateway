import { Control } from "../api.js";
import {
  $,
  badge,
  confirmDialog,
  errText,
  fillSlot,
  go,
  html,
  join,
  mountView,
  raw,
  sameView,
  toast,
  when,
  withBusy,
} from "../lib.js";
import { accountHref, loginAttemptsHref, loginHref } from "../router.js";
import {
  findLogin,
  pendingCount,
  refreshAll,
  sortedLogins,
  state,
} from "../state.js";
import { empty } from "./helpers.js";

function attemptActions(attempt) {
  // Return raw so nesting inside another html`` does not escape the buttons.
  return raw(
    html` ${when(
        attempt.url,
        () =>
          html` <a
              class="btn primary"
              href="${attempt.url}"
              target="_blank"
              rel="noopener"
              >Open</a
            >
            <button class="btn ghost" type="button" data-copy="${attempt.url}">
              Copy
            </button>`,
      )}
      <button class="btn danger" type="button" data-del-login="${attempt.id}">
        Close
      </button>`,
  );
}

function loginRow(attempt) {
  return html` <div class="row ${attempt.state === "pending" ? "pending" : ""}">
    <a class="row-main" href="${loginHref(attempt.id)}">
      <div class="id">${attempt.id}</div>
      <div class="meta">
        ${badge(attempt.state, attempt.state)}
        ${when(attempt.account_id, () => html`<span>account ${attempt.account_id}</span>`)}
        ${when(attempt.error, () => html`<span>${attempt.error}</span>`)}
      </div>
    </a>
    <div class="actions">${attemptActions(attempt)}</div>
  </div>`;
}

function loginListMarkup() {
  const list = sortedLogins();
  if (!list.length) {
    return empty("No login attempts yet. Create one to get an Open/Copy URL.");
  }
  return html`<div class="rows">${join(list, loginRow)}</div>`;
}

function createAttemptButton() {
  const s = state.service;
  const pending = pendingCount();
  const cap = s.max_login_attempts || 3;
  const atCap = pending >= cap;
  return html` <div class="create-block">
    <button
      class="btn primary"
      id="create-login-attempt"
      type="button"
      ${when(atCap, "disabled")}
    >
      Create login attempt
    </button>
    ${when(atCap, () => html`<p class="hint">At the open-attempt cap. Close a pending attempt first.</p>`)}
  </div>`;
}

function loginBanner(attempt) {
  if (attempt.state === "pending") {
    return html`<div class="banner warn">
      Waiting for the browser login to finish…
    </div>`;
  }
  if (attempt.state === "succeeded") {
    if (attempt.account_id) {
      return html`<div class="banner ok">
        Login finished. Account
        <a href="${accountHref(attempt.account_id)}">${attempt.account_id}</a>
        added.
      </div>`;
    }
    return html`<div class="banner ok">Login finished. Account added.</div>`;
  }
  if (attempt.state === "failed") {
    return html`<div class="banner bad">
      ${attempt.error || "Login failed."}
    </div>`;
  }
  if (attempt.state === "expired") {
    return html`<div class="banner bad">
      This attempt expired. Create a new login attempt.
    </div>`;
  }
  return "";
}

function loginKv(attempt) {
  return html` <dl class="kv">
      <dt>id</dt>
      <dd>${attempt.id}</dd>
      <dt>state</dt>
      <dd>${badge(attempt.state, attempt.state)}</dd>
      <dt>account</dt>
      <dd>
        ${when(attempt.account_id, () => html`<a href="${accountHref(attempt.account_id)}">${attempt.account_id}</a>`)}${attempt.account_id ? "" : "—"}
      </dd>
      <dt>error</dt>
      <dd>${attempt.error || "—"}</dd>
    </dl>
    ${when(attempt.url, () => html`<div class="url-box">${attempt.url}</div>`)}
    <p class="hint">
      <code>GET /api/login-attempts/{id}</code>. Succeeded attempts stay listed
      for the keep window.
    </p>`;
}

async function renderLoginDetail(page, route, { patch, stale }) {
  const cached = findLogin(route.id);
  if (!patch || !cached) {
    try {
      state.attempt = (await Control.loginAttempt(route.id)).data;
    } catch (error) {
      if (stale()) return;
      mountView(
        page,
        "login/detail/" + route.id,
        html` <div class="head">
          <div>
            <h2>Login attempt</h2>
            <p class="err">${errText(error)}</p>
          </div>
          <a class="btn ghost" href="${loginAttemptsHref()}"
            >Back to attempts</a
          >
        </div>`,
      );
      return;
    }
    if (stale()) return;
  } else {
    state.attempt = cached;
  }

  const attempt = state.attempt;
  const signature = "login/detail/" + route.id;
  if (!patch || !sameView(page, signature)) {
    mountView(
      page,
      signature,
      html` <div class="head">
          <div>
            <h2>Login attempt</h2>
            <p>
              Resource <code>/api/login-attempts/${attempt.id}</code>.
              Completing the URL in the browser adds the account to the pool.
            </p>
          </div>
          <div class="actions" data-slot="actions"></div>
        </div>
        <div data-slot="banner"></div>
        <div class="card"><div class="body" data-slot="kv"></div></div>`,
    );
  }
  fillSlot(page, "banner", loginBanner(attempt));
  fillSlot(page, "actions", attemptActions(attempt));
  fillSlot(page, "kv", loginKv(attempt));
}

function renderLoginList(page, { patch }) {
  const s = state.service;
  if (!patch || !sameView(page, "login/attempts")) {
    mountView(
      page,
      "login/attempts",
      html` <div class="head">
          <div>
            <h2>Login attempts</h2>
            <p data-slot="lede"></p>
          </div>
          <div class="actions" data-slot="create"></div>
        </div>
        <div class="card"><div class="body" data-slot="list"></div></div>`,
    );
  }
  fillSlot(
    page,
    "lede",
    html`<code>GET/POST /api/login-attempts</code>. Cap
      ${s.max_login_attempts ?? 3} open. Unanswered close after
      ${s.login_attempt_mins ?? 3} minutes. ${pendingCount()} pending now.`,
  );
  fillSlot(page, "create", createAttemptButton());
  fillSlot(page, "list", loginListMarkup());
}

export function renderLogin(page, route, opts) {
  if (route.mode === "detail") return renderLoginDetail(page, route, opts);
  return renderLoginList(page, opts);
}

export async function createLoginAttempt() {
  const btn = $("create-login-attempt");
  await withBusy(btn, async () => {
    await Control.createLoginAttempt();
    toast("login attempt created");
    await refreshAll();
    // Stay on the attempts list; re-render so the new resource appears.
    go(loginAttemptsHref());
  });
}

export async function closeLogin(id) {
  if (!(await confirmDialog("Close login attempt " + id + "?"))) return;
  await Control.deleteLoginAttempt(id);
  toast("login attempt closed");
  await refreshAll();
  go(loginAttemptsHref());
}
