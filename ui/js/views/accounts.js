import { Control } from "../api.js";
import {
  $,
  badge,
  confirmDialog,
  errText,
  fillSlot,
  fmtTime,
  go,
  html,
  join,
  mountView,
  sameView,
  toast,
  when,
  withBusy,
} from "../lib.js";
import { accountHref } from "../router.js";
import { findAccount, refreshAll, state } from "../state.js";
import { empty } from "./helpers.js";

function accountRow(account) {
  const search = [account.id, account.subject, account.tier]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
  return html` <div class="row" data-search="${search}">
    <a class="row-main" href="${accountHref(account.id)}">
      <div class="id">${account.id}</div>
      <div class="meta">
        ${badge(account.tier || "unknown", account.tier || "unknown")}
        ${when(account.subject && account.subject !== account.id, () => html`<span>${account.subject}</span>`)}
        <span title="${fmtTime(account.expires)}"
          >${fmtTime(account.expires)}</span
        >
      </div>
    </a>
    <div class="actions">
      <button class="btn ghost" type="button" data-copy="${account.id}">
        Copy id
      </button>
      <button class="btn danger" type="button" data-del-account="${account.id}">
        Remove
      </button>
    </div>
  </div>`;
}

function applyAccountFilter(page) {
  const input = page.querySelector("#account-filter");
  const query = (input?.value || "").trim().toLowerCase();
  let shown = 0;
  page.querySelectorAll("[data-search]").forEach((row) => {
    const hide = Boolean(query) && !row.dataset.search.includes(query);
    row.hidden = hide;
    if (!hide) shown += 1;
  });
  const emptyEl = page.querySelector("[data-filter-empty]");
  if (emptyEl) emptyEl.hidden = !query || shown > 0;
}

function accountListMarkup() {
  if (!state.accounts.length)
    return empty("No accounts in the pool.", "#/accounts/add", "Add tokens");
  return html` <div class="rows">${join(state.accounts, accountRow)}</div>
    <div class="empty" data-filter-empty hidden>
      No accounts match that filter.
    </div>`;
}

function bindAccountForm(formId, readBody) {
  const form = $(formId);
  if (!form) return;
  form.onsubmit = async (event) => {
    event.preventDefault();
    const err = $("form-error");
    if (err) {
      err.hidden = true;
      err.textContent = "";
    }
    const submit = form.querySelector("[type=submit]");
    await withBusy(submit, async () => {
      try {
        const body = readBody();
        if (body == null) return;
        const res = await Control.addAccount(body);
        toast(
          res.status === 200 ? "merged existing account" : "account stored",
        );
        form.reset();
        await refreshAll();
        go(accountHref((res.data && res.data.id) || ""));
      } catch (error) {
        if (err) {
          err.hidden = false;
          err.textContent = errText(error);
        }
        toast(errText(error), true);
      }
    });
  };
}

function renderAddTokens(page, { patch }) {
  if (patch && sameView(page, "accounts/add")) return;
  mountView(
    page,
    "accounts/add",
    html` <div class="head">
        <div>
          <h2>Add tokens</h2>
          <p>
            Refresh token is required. The gateway tests it against Cursor, then
            stores or merges by subject.
          </p>
        </div>
      </div>
      <div class="card">
        <form class="body" id="token-form">
          <label
            >Refresh token
            <input
              id="refresh-token"
              type="password"
              autocomplete="off"
              spellcheck="false"
              required
            />
          </label>
          <label
            >Access token <span class="hint">(optional)</span>
            <input
              id="access-token"
              type="password"
              autocomplete="off"
              spellcheck="false"
            />
          </label>
          <p class="err" id="form-error" hidden></p>
          <div class="actions">
            <button class="btn primary" type="submit">Test and store</button>
            <a class="btn ghost" href="#/accounts">Back to pool</a>
          </div>
        </form>
      </div>`,
  );
  bindAccountForm("token-form", () => {
    const body = { refreshToken: $("refresh-token").value.trim() };
    const access = $("access-token").value.trim();
    if (access) body.accessToken = access;
    return body;
  });
}

function renderImportJSON(page, { patch }) {
  if (patch && sameView(page, "accounts/import")) return;
  mountView(
    page,
    "accounts/import",
    html` <div class="head">
        <div>
          <h2>Import JSON</h2>
          <p>
            Accepts <code>refreshToken</code>/<code>refresh</code>, optional
            access fields, or nested <code>{"cursor":{...}}</code>.
          </p>
        </div>
      </div>
      <div class="card">
        <form class="body" id="json-form">
          <label
            >Payload
            <textarea
              id="json-body"
              required
              spellcheck="false"
              placeholder='{ "refreshToken": "..." }'
            ></textarea>
          </label>
          <p class="err" id="form-error" hidden></p>
          <div class="actions">
            <button class="btn primary" type="submit">Test and store</button>
            <a class="btn ghost" href="#/accounts">Back to pool</a>
          </div>
        </form>
      </div>`,
  );
  bindAccountForm("json-form", () => {
    try {
      return JSON.parse($("json-body").value);
    } catch (error) {
      const message = "invalid JSON: " + error.message;
      const err = $("form-error");
      if (err) {
        err.hidden = false;
        err.textContent = message;
      }
      toast(message, true);
      return null;
    }
  });
}

function renderAccountPool(page, { patch }) {
  if (!patch || !sameView(page, "accounts/pool")) {
    mountView(
      page,
      "accounts/pool",
      html` <div class="head">
          <div>
            <h2>Account pool</h2>
            <p>
              Local sessions from <code>GET /api/accounts</code>. Remove deletes
              the store entry only.
            </p>
          </div>
          <a class="btn primary" href="#/accounts/add">Add tokens</a>
        </div>
        <div class="card">
          <div class="body">
            <input
              id="account-filter"
              class="filter"
              type="search"
              placeholder="Filter by id, subject, or tier"
              autocomplete="off"
            />
            <div data-slot="list"></div>
          </div>
        </div>`,
    );
    $("account-filter").oninput = () => applyAccountFilter(page);
  }
  fillSlot(page, "list", accountListMarkup());
  applyAccountFilter(page);
}

function accountKv(account) {
  return html` <dl class="kv">
    <dt>id</dt>
    <dd>${account.id}</dd>
    <dt>subject</dt>
    <dd>${account.subject || "—"}</dd>
    <dt>tier</dt>
    <dd>${badge(account.tier || "unknown", account.tier || "unknown")}</dd>
    <dt>expires</dt>
    <dd>${fmtTime(account.expires)}</dd>
  </dl>`;
}

async function renderAccountDetail(page, route, { patch, stale }) {
  const cached = findAccount(route.id);
  if (!patch || !cached) {
    try {
      state.account = (await Control.account(route.id)).data;
    } catch (error) {
      if (stale()) return;
      mountView(
        page,
        "accounts/detail/" + route.id,
        html` <div class="head">
          <div>
            <h2>Account</h2>
            <p class="err">${errText(error)}</p>
          </div>
        </div>`,
      );
      return;
    }
    if (stale()) return;
  } else {
    state.account = cached;
  }

  const acc = state.account;
  const signature = "accounts/detail/" + route.id;
  if (!patch || !sameView(page, signature)) {
    mountView(
      page,
      signature,
      html` <div class="head">
          <div>
            <h2>Account</h2>
            <p>
              Public id is the JWT subject when present, otherwise the store
              UUID.
            </p>
          </div>
          <div class="actions">
            <button class="btn ghost" type="button" data-copy="${acc.id}">
              Copy id
            </button>
            <button
              class="btn danger"
              type="button"
              data-del-account="${acc.id}"
            >
              Remove
            </button>
          </div>
        </div>
        <div class="card"><div class="body" data-slot="kv"></div></div>`,
    );
  }
  fillSlot(page, "kv", accountKv(acc));
}

export function renderAccounts(page, route, opts) {
  if (route.mode === "add") return renderAddTokens(page, opts);
  if (route.mode === "import") return renderImportJSON(page, opts);
  if (route.mode === "detail") return renderAccountDetail(page, route, opts);
  return renderAccountPool(page, opts);
}

export async function removeAccount(id) {
  if (
    !(await confirmDialog(
      "Remove account " + id + "? This deletes the local session only.",
    ))
  )
    return;
  await Control.deleteAccount(id);
  toast("account removed");
  await refreshAll();
  go("#/accounts");
}
