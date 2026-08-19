import { AI, ApiError } from "../api.js";
import {
  $,
  badge,
  copyText,
  errText,
  fillSlot,
  html,
  join,
  mountView,
  raw,
  sameView,
  toast,
  when,
  withBusy,
} from "../lib.js";
import { aiTestHref } from "../router.js";
import { empty } from "./helpers.js";

const AUTO_MODEL = "default";
const DEFAULT_PROMPT = "Reply with the single word pong.";

const cache = {
  models: null,
  modelsAt: 0,
  modelsMs: null,
  modelsError: null,
  modelsLoading: false,
  lastTest: null,
  form: null,
  abort: null,
};

let tickTimer = 0;

function isAuto(model) {
  const id = (model && model.id) || "";
  const name = (model && model.name) || "";
  return id === AUTO_MODEL || id === "auto" || name.toLowerCase() === "auto";
}

function sortedModels() {
  return (cache.models || []).slice().sort((a, b) => {
    const da = isAuto(a) ? 0 : 1;
    const db = isAuto(b) ? 0 : 1;
    if (da !== db) return da - db;
    return String(a.id || "").localeCompare(String(b.id || ""));
  });
}

function pretty(value) {
  if (value == null) return "";
  if (typeof value === "string") {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return value;
    }
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function fmtMs(ms) {
  if (ms == null || Number.isNaN(Number(ms))) return "—";
  const n = Number(ms);
  if (n < 1000) return n + "ms";
  return (n / 1000).toFixed(n < 10000 ? 1 : 0) + "s";
}

function fmtAgo(ts) {
  if (!ts) return "";
  const d = Date.now() - ts;
  if (d < 8000) return "just now";
  if (d < 60000) return Math.round(d / 1000) + "s ago";
  return new Date(ts).toLocaleTimeString();
}

function kv(pairs) {
  const items = (pairs || []).filter(
    (pair) => pair && pair[1] != null && pair[1] !== "",
  );
  if (!items.length) return "";
  return raw(
    html`<dl class="kv">
      ${join(
        items,
        ([key, value]) =>
          html`<dt>${key}</dt>
            <dd>${value}</dd>`,
      )}
    </dl>`,
  );
}

function errorFields(error) {
  const data = error && error.data;
  const obj =
    data && data.error && typeof data.error === "object" ? data.error : null;
  return {
    status: error && error.status,
    type: obj && obj.type,
    code: obj && obj.code,
    path: error && error.path,
    method: error && error.method,
    ms: error && error.ms,
    network: Boolean(error && error.network),
    aborted: Boolean(error && error.aborted),
    raw: error && error.raw,
    data,
  };
}

function errorHint(error) {
  const msg = errText(error).toLowerCase();
  const status = error && error.status;
  if (error && error.aborted)
    return "The request was cancelled before it finished.";
  if (error && error.network)
    return "Serve this page on the same origin as cursed-gateway, or proxy /ai here.";
  if (msg.includes("no sessions") || msg.includes("auth store"))
    return "Import or login an account first. /ai needs a session in the pool.";
  if (msg.includes("no user message"))
    return "The user prompt was empty after the gateway parsed messages.";
  if (msg.includes("messages is required"))
    return "Chat completions needs a messages array with at least one user turn.";
  if (msg.includes("invalid json"))
    return "The gateway could not parse the JSON body.";
  if (status === 401 || status === 403)
    return "The gateway rejected the request. Check that a pool session exists.";
  if (status === 429)
    return "Rate limited. Wait and retry, or add another account.";
  if (status === 409)
    return "Conflict. Another request may be using this conversation.";
  if (status === 502 || status === 503 || status === 504)
    return "Upstream/Cursor failed. The body below is what /ai returned.";
  if (status === 400)
    return "The gateway rejected the body. See type/code and the error body below.";
  if (status >= 500)
    return "Unexpected gateway error. Details below are from the /ai response.";
  return "This error is from /ai (or the browser network layer), not the console.";
}

function errorCard(
  error,
  { title = "Error", retry = "", copyKind = "error" } = {},
) {
  const fields = errorFields(error);
  const kind = fields.aborted
    ? "cancelled"
    : fields.network
      ? "network"
      : fields.status
        ? "HTTP " + fields.status
        : "error";
  return html` <div class="card" role="alert">
    <h3>${title}</h3>
    <div class="body">
      <div class="banner bad">
        <div>
          <strong>${kind}</strong>
          <div>${errText(error)}</div>
        </div>
      </div>
      <p class="hint">${errorHint(error)}</p>
      ${kv([
        ["status", fields.status],
        ["type", fields.type],
        ["code", fields.code],
        ["method", fields.method],
        ["path", fields.path],
        ["elapsed", fields.ms != null ? fmtMs(fields.ms) : ""],
      ])}
      ${when(
        fields.data || fields.raw,
        () =>
          html` <details class="inspect" open>
            <summary>Error body</summary>
            <div class="output">${pretty(fields.data || fields.raw)}</div>
          </details>`,
      )}
      <div class="actions">
        <button
          class="btn ghost"
          type="button"
          data-action="copy-ai-json"
          data-ai-json="${copyKind}"
        >
          Copy error JSON
        </button>
        ${when(
          retry,
          () =>
            html`<button
              class="btn primary"
              type="button"
              data-action="${retry}"
            >
              Retry
            </button>`,
        )}
      </div>
    </div>
  </div>`;
}

function inspectDetails(last) {
  return raw(
    html`${when(
      last.request,
      () =>
        html` <details class="inspect">
          <summary>Request JSON</summary>
          <div class="output">${pretty(last.request)}</div>
        </details>`,
    )}
    ${when(
      last.data,
      () =>
        html` <details class="inspect">
          <summary>Response JSON</summary>
          <div class="output">${pretty(last.data)}</div>
        </details>`,
    )}`,
  );
}

function cursorPairs(headers) {
  if (!headers) return [];
  return [
    ["cursor model", headers["x-cursor-model"]],
    ["wire model", headers["x-cursor-wire-model"]],
    ["params", headers["x-cursor-model-params"]],
    ["content-type", headers["content-type"]],
  ];
}

function usageText(usage) {
  if (!usage) return "";
  return (
    (usage.prompt_tokens ?? "—") +
    "/" +
    (usage.completion_tokens ?? "—") +
    " (" +
    (usage.total_tokens ?? "—") +
    ")"
  );
}

function modelRow(model) {
  const search = [model.id, model.name, model.owned_by]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
  const name = model.name || model.id;
  return html` <div class="row" data-search="${search}">
    <div class="row-main">
      <div class="id">${model.id}</div>
      <div class="meta">
        ${when(isAuto(model), () => html`${badge("unknown", "Auto")}`)}
        ${when(name && name !== model.id, () => html`<span>${name}</span>`)}
        ${when(model.owned_by, () => html`<span>${model.owned_by}</span>`)}
      </div>
    </div>
    <div class="actions">
      <button class="btn ghost" type="button" data-copy="${model.id}">
        Copy id
      </button>
      <a class="btn primary" href="${aiTestHref(model.id)}">Test</a>
    </div>
  </div>`;
}

function applyModelFilter(page) {
  const input = page.querySelector("#model-filter");
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

function modelsBannerMarkup() {
  if (cache.modelsLoading)
    return html`<div class="banner warn" role="status">
      Loading GET /ai/v1/models…
    </div>`;
  if (cache.modelsError)
    return errorCard(cache.modelsError, {
      title: "Catalog failed",
      retry: "list-models",
      copyKind: "catalog",
    });
  return "";
}

function modelsStatusMarkup() {
  if (cache.models == null)
    return html`<p class="hint">
      Catalog is not loaded until you list models. A cold catalog may call
      Cursor.
    </p>`;
  const auto = cache.models.some(isAuto);
  const bits = [
    cache.models.length + " models",
    auto ? "Auto present" : "Auto missing",
    cache.modelsAt ? fmtAgo(cache.modelsAt) : "",
    cache.modelsMs != null ? fmtMs(cache.modelsMs) : "",
  ].filter(Boolean);
  return html` <div class="actions">
    <p class="hint">${bits.join(" · ")}</p>
    <button
      class="btn ghost"
      type="button"
      data-action="copy-ai-json"
      data-ai-json="models"
    >
      Copy catalog JSON
    </button>
  </div>`;
}

function modelListMarkup() {
  if (cache.models == null)
    return empty("Catalog not loaded. List models to call GET /ai/v1/models.");
  if (!cache.models.length) return empty("No models returned.");
  return html` <div class="rows">${join(sortedModels(), modelRow)}</div>
    <div class="empty" data-filter-empty hidden>
      No models match that filter.
    </div>`;
}

function modelOptions() {
  return join(
    sortedModels(),
    (model) =>
      html`<option
        value="${model.id}"
        label="${model.name || model.id}"
      ></option>`,
  );
}

function fillModelList() {
  const list = $("ai-model-list");
  if (!list) return;
  const options = modelOptions();
  list.innerHTML = options && options.__html != null ? options.__html : "";
}

function testNoteMarkup() {
  if (cache.modelsLoading)
    return html`<div class="banner warn" role="status">
      Loading GET /ai/v1/models…
    </div>`;
  if (cache.modelsError)
    return errorCard(cache.modelsError, {
      title: "Catalog failed",
      retry: "list-models",
      copyKind: "catalog",
    });
  return "";
}

function paintModels() {
  const page = $("page");
  if (!page || !sameView(page, "ai/models")) return;
  fillSlot(page, "models-banner", modelsBannerMarkup());
  fillSlot(page, "models-status", modelsStatusMarkup());
  fillSlot(page, "list", modelListMarkup());
  applyModelFilter(page);
}

function paintTestNote() {
  const page = $("page");
  if (!page || !sameView(page, "ai/test")) return;
  fillSlot(page, "ai-note", testNoteMarkup());
  fillModelList();
}

function completionText(last) {
  const data = last.data || {};
  const choice = (data.choices && data.choices[0]) || {};
  const msg = choice.message || {};
  return msg.content || last.text || "";
}

function testResultMarkup() {
  const last = cache.lastTest;
  if (!last) return "";
  if (last.pending) {
    return html` <div class="card">
      <h3>${last.stream ? "Streaming" : "Running"}</h3>
      <div class="body">
        <div class="banner warn" role="status">
          <div>
            <strong>${last.stream ? "sse" : "non-stream"}</strong>
            <div>
              ${last.path || "/ai"} ·
              <span id="ai-elapsed">${fmtMs(Date.now() - last.startedAt)}</span>
            </div>
          </div>
        </div>
        <div class="output" data-slot="ai-output">${last.text || ""}</div>
      </div>
    </div>`;
  }
  if (!last.ok) {
    return html`${raw(errorCard(last.error, { title: "Request failed" }))}
    ${when(
      last.text,
      () =>
        html` <div class="card">
          <h3>Partial output</h3>
          <div class="body">
            <div class="output">${last.text}</div>
          </div>
        </div>`,
    )}
    ${when(
      last.request,
      () =>
        html` <div class="card">
          <h3>Request</h3>
          <div class="body">
            ${inspectDetails(last)}
            <div class="actions">
              <button
                class="btn ghost"
                type="button"
                data-action="copy-ai-json"
                data-ai-json="request"
              >
                Copy request
              </button>
            </div>
          </div>
        </div>`,
    )}`;
  }

  const data = last.data || {};
  const choice = (data.choices && data.choices[0]) || {};
  const msg = choice.message || {};
  const text = completionText(last);
  const finish = choice.finish_reason || "—";
  const tools = (msg.tool_calls && msg.tool_calls.length) || 0;
  return html` <div class="card">
    <h3>Completion</h3>
    <div class="body">
      ${when(
        last.incomplete,
        () =>
          html`<div class="banner warn">
            Stream ended without a [DONE] event. Showing tokens received.
          </div>`,
      )}
      ${kv(
        [
          ["id", data.id],
          ["model", data.model || (last.form && last.form.model)],
          ["path", last.path],
          ["status", last.status],
          ["elapsed", fmtMs(last.ms)],
          ["finish", finish],
          ["stream", last.stream ? "sse" : "false"],
          ...cursorPairs(last.headers),
          ["tokens", usageText(data.usage)],
          tools ? ["tool calls", tools] : null,
        ].filter(Boolean),
      )}
      <div class="output">${text || "(empty)"}</div>
      ${inspectDetails(last)}
      <div class="actions">
        <button
          class="btn ghost"
          type="button"
          data-action="copy-ai-json"
          data-ai-json="response"
        >
          Copy response
        </button>
        <button
          class="btn ghost"
          type="button"
          data-action="copy-ai-json"
          data-ai-json="request"
        >
          Copy request
        </button>
      </div>
    </div>
  </div>`;
}

function stopTick() {
  clearInterval(tickTimer);
  tickTimer = 0;
}

function startTick() {
  stopTick();
  tickTimer = setInterval(() => {
    const el = $("ai-elapsed");
    const last = cache.lastTest;
    if (!el || !last || !last.pending) {
      stopTick();
      return;
    }
    el.textContent = fmtMs(Date.now() - last.startedAt);
  }, 250);
}

function setTestBusy(busy) {
  const submit = document.querySelector("#ai-test-form [type=submit]");
  const cancel = $("ai-cancel");
  if (submit) submit.disabled = busy;
  if (cancel) {
    cancel.hidden = !busy;
    cancel.disabled = !busy;
  }
}

function syncEndpointUi() {
  const completions = $("ai-endpoint")?.value === "completions";
  const system = $("ai-system");
  if (system) system.disabled = completions;
  const hint = $("ai-system-hint");
  if (hint) hint.hidden = !completions;
}

function readForm() {
  const model = ($("ai-model")?.value || "").trim() || AUTO_MODEL;
  const system = ($("ai-system")?.value || "").trim();
  const prompt = ($("ai-prompt")?.value || "").trim();
  const endpoint =
    $("ai-endpoint")?.value === "completions" ? "completions" : "chat";
  const stream = Boolean($("ai-stream")?.checked);
  cache.form = { model, system, prompt, endpoint, stream };
  return cache.form;
}

function buildRequest(form) {
  if (form.endpoint === "completions") {
    return {
      path: "/ai/v1/completions",
      body: { model: form.model, prompt: form.prompt, stream: form.stream },
    };
  }
  const messages = [];
  if (form.system) messages.push({ role: "system", content: form.system });
  messages.push({ role: "user", content: form.prompt });
  return {
    path: "/ai/v1/chat/completions",
    body: { model: form.model, messages, stream: form.stream },
  };
}

function paintResult(page) {
  fillSlot(page, "result", testResultMarkup());
}

async function runTest(page) {
  const form = readForm();
  if (!form.prompt) {
    cache.lastTest = {
      ok: false,
      error: new ApiError("prompt is required", {
        status: 400,
        data: {
          error: {
            message: "prompt is required",
            type: "invalid_request_error",
            code: "bad_request",
          },
        },
      }),
      form,
      request: null,
    };
    paintResult(page);
    return;
  }

  const { path, body } = buildRequest(form);
  const prev = cache.abort;
  const ac = new AbortController();
  cache.abort = ac;
  if (prev) prev.abort();

  cache.lastTest = {
    ok: false,
    pending: true,
    stream: form.stream,
    path,
    request: body,
    form,
    text: "",
    startedAt: Date.now(),
  };
  setTestBusy(true);
  paintResult(page);
  startTick();

  try {
    const extra = { signal: ac.signal };
    let res;
    if (form.stream) {
      extra.onEvent = (evt) => {
        if (cache.abort !== ac) return;
        cache.lastTest.text = evt.text || "";
        const out = page.querySelector("[data-slot=ai-output]");
        if (out) out.textContent = cache.lastTest.text;
      };
      res = await AI.stream(path, body, extra);
    } else if (form.endpoint === "completions") {
      res = await AI.completions(body, extra);
    } else {
      res = await AI.chat(body, extra);
    }
    if (cache.abort !== ac) return;
    const data = res.data || {};
    cache.lastTest = {
      ok: true,
      pending: false,
      stream: form.stream,
      incomplete: Boolean(form.stream && res.streamed && res.done === false),
      path,
      request: body,
      form,
      data,
      headers: res.headers || {},
      status: res.status,
      ms: res.ms,
      text: completionText({ data, text: res.text }),
    };
    paintResult(page);
    toast("completion finished");
  } catch (error) {
    if (cache.abort !== ac) return;
    cache.lastTest = {
      ok: false,
      pending: false,
      stream: form.stream,
      path,
      request: body,
      form,
      error,
      text: (cache.lastTest && cache.lastTest.text) || "",
      ms: error && error.ms,
    };
    paintResult(page);
    if (!(error && error.aborted)) toast(errText(error), true);
  } finally {
    if (cache.abort === ac) {
      cache.abort = null;
      setTestBusy(false);
      stopTick();
    }
  }
}

function bindTestForm(page) {
  const form = $("ai-test-form");
  if (!form) return;
  form.onsubmit = async (event) => {
    event.preventDefault();
    await runTest(page);
  };
  form.oninput = () => readForm();
  const endpoint = $("ai-endpoint");
  if (endpoint) endpoint.onchange = syncEndpointUi;
}

function restoreForm(route) {
  const form = cache.form || {};
  const model = (route && route.id) || form.model || AUTO_MODEL;
  const modelEl = $("ai-model");
  const systemEl = $("ai-system");
  const promptEl = $("ai-prompt");
  const endpointEl = $("ai-endpoint");
  const streamEl = $("ai-stream");
  if (modelEl) modelEl.value = model;
  if (systemEl) systemEl.value = form.system || "";
  if (promptEl) promptEl.value = form.prompt || DEFAULT_PROMPT;
  if (endpointEl) endpointEl.value = form.endpoint || "chat";
  if (streamEl) streamEl.checked = Boolean(form.stream);
  syncEndpointUi();
  fillModelList();
}

function renderModels(page, { patch }) {
  if (!patch || !sameView(page, "ai/models")) {
    mountView(
      page,
      "ai/models",
      html` <div class="head">
          <div>
            <h2>Models</h2>
            <p>
              Same-origin <code>GET /ai/v1/models</code>. Default Auto is id
              <code>default</code>. Listing is manual; a cold catalog may call
              Cursor.
            </p>
          </div>
          <button class="btn primary" type="button" data-action="list-models">
            List models
          </button>
        </div>
        <div data-slot="models-banner"></div>
        <div class="card">
          <div class="body">
            <div data-slot="models-status"></div>
            <input
              id="model-filter"
              class="filter"
              type="search"
              placeholder="Filter by id or name"
              autocomplete="off"
            />
            <div data-slot="list"></div>
          </div>
        </div>`,
    );
    $("model-filter").oninput = () => applyModelFilter(page);
  }
  paintModels();
}

function renderTest(page, route, { patch }) {
  if (!patch || !sameView(page, "ai/test")) {
    mountView(
      page,
      "ai/test",
      html` <div class="head">
          <div>
            <h2>Test</h2>
            <p>
              Same-origin <code>/ai/v1/chat/completions</code> or
              <code>/ai/v1/completions</code>. Default Auto is
              <code>${AUTO_MODEL}</code>. This is a live Cursor call.
            </p>
          </div>
          <div class="actions">
            <button class="btn ghost" type="button" data-action="list-models">
              List models
            </button>
            <a class="btn ghost" href="#/ai">Back to models</a>
          </div>
        </div>
        <div data-slot="ai-note"></div>
        <div class="card">
          <form class="body" id="ai-test-form">
            <label
              >Model
              <input
                id="ai-model"
                list="ai-model-list"
                spellcheck="false"
                autocomplete="off"
                placeholder="${AUTO_MODEL}"
              />
              <datalist id="ai-model-list"></datalist>
            </label>
            <div class="field-row">
              <label
                >Endpoint
                <select id="ai-endpoint">
                  <option value="chat">POST /ai/v1/chat/completions</option>
                  <option value="completions">POST /ai/v1/completions</option>
                </select>
              </label>
              <label class="check"
                ><input id="ai-stream" type="checkbox" /> Stream SSE</label
              >
            </div>
            <label
              >System prompt <span class="hint">(optional, chat only)</span>
              <textarea
                id="ai-system"
                class="short"
                spellcheck="true"
              ></textarea>
            </label>
            <p class="hint" id="ai-system-hint" hidden>
              Completions sends <code>prompt</code> only; the system field is
              ignored.
            </p>
            <label
              >User prompt
              <textarea id="ai-prompt" required spellcheck="true"></textarea>
            </label>
            <div class="actions">
              <button class="btn primary" type="submit">Run test</button>
              <button
                class="btn danger"
                type="button"
                id="ai-cancel"
                data-action="cancel-ai-test"
                hidden
              >
                Cancel
              </button>
            </div>
          </form>
        </div>
        <div data-slot="result"></div>`,
    );
    restoreForm(route);
    bindTestForm(page);
  } else if (route && route.id && $("ai-model")) {
    $("ai-model").value = route.id;
  }
  paintTestNote();
  paintResult(page);
  if (cache.lastTest && cache.lastTest.pending) {
    setTestBusy(true);
    startTick();
  } else {
    setTestBusy(false);
  }
}

export function renderAI(page, route, opts) {
  if (route.mode === "test") return renderTest(page, route, opts);
  return renderModels(page, opts);
}

export async function listModels() {
  const btn = document.querySelector("[data-action=list-models]");
  const run = async () => {
    cache.modelsLoading = true;
    cache.modelsError = null;
    paintModels();
    paintTestNote();
    try {
      const res = await AI.models();
      cache.models = (res.data && res.data.data) || [];
      cache.modelsAt = Date.now();
      cache.modelsMs = res.ms;
      cache.modelsError = null;
      toast(cache.models.length + " models");
    } catch (error) {
      cache.modelsError = error;
      toast(errText(error), true);
    } finally {
      cache.modelsLoading = false;
      paintModels();
      paintTestNote();
    }
  };
  if (btn && !btn.disabled) await withBusy(btn, run);
  else await run();
}

export function cancelAITest() {
  if (cache.abort) cache.abort.abort();
}

export function copyAIJson(kind) {
  let value = null;
  if (kind === "models") value = cache.models;
  else if (kind === "catalog")
    value =
      cache.modelsError &&
      (cache.modelsError.data || { message: errText(cache.modelsError) });
  else if (kind === "request") value = cache.lastTest && cache.lastTest.request;
  else if (kind === "response") value = cache.lastTest && cache.lastTest.data;
  else if (kind === "error")
    value =
      cache.lastTest &&
      cache.lastTest.error &&
      (cache.lastTest.error.data || {
        message: errText(cache.lastTest.error),
      });
  if (value == null) {
    toast("nothing to copy", true);
    return;
  }
  copyText(pretty(value));
}
