export class ApiError extends Error {
  constructor(message, extra = {}) {
    super(message);
    this.name = "ApiError";
    Object.assign(this, extra);
  }
}

export async function api(method, path, body, extra = {}) {
  const started = performance.now();
  const opts = {
    method,
    headers: { Accept: extra.accept || "application/json" },
  };
  if (extra.signal) opts.signal = extra.signal;
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = typeof body === "string" ? body : JSON.stringify(body);
  }

  let res;
  try {
    res = await fetch(path, opts);
  } catch (err) {
    throw fetchError(err, method, path, started);
  }

  const text = await res.text();
  const ms = elapsed(started);
  const headers = headerMap(res);
  const data = decodeBody(text);
  if (!res.ok) {
    throw new ApiError(apiMessage(data, res.status + " " + res.statusText), {
      status: res.status,
      data,
      raw: text,
      headers,
      ms,
      method,
      path,
    });
  }
  return { status: res.status, data, headers, ms, raw: text };
}

function elapsed(started) {
  return Math.max(0, Math.round(performance.now() - started));
}

function headerMap(res) {
  const out = {};
  if (!res || !res.headers) return out;
  res.headers.forEach((value, key) => {
    out[key.toLowerCase()] = value;
  });
  return out;
}

function decodeBody(text) {
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return { error: text };
  }
}

export function apiMessage(data, fallback) {
  if (!data) return fallback;
  const err = data.error;
  if (typeof err === "string" && err) return err;
  if (err && typeof err.message === "string" && err.message) return err.message;
  if (typeof data.message === "string" && data.message) return data.message;
  return fallback;
}

function isAbort(err) {
  return Boolean(err && (err.name === "AbortError" || err.code === 20));
}

function fetchError(err, method, path, started) {
  const ms = elapsed(started);
  if (isAbort(err)) {
    return new ApiError("request cancelled", {
      aborted: true,
      ms,
      method,
      path,
    });
  }
  return new ApiError("cannot reach " + path + " (" + err.message + ")", {
    network: true,
    ms,
    method,
    path,
  });
}

async function readSSE(res, onEvent) {
  const reader = res.body && res.body.getReader();
  if (!reader) throw new Error("no response body");
  const decoder = new TextDecoder();
  let buf = "";
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true }).replace(/\r\n/g, "\n");
    let split;
    while ((split = buf.indexOf("\n\n")) >= 0) {
      const block = buf.slice(0, split);
      buf = buf.slice(split + 2);
      const dataLines = block
        .split("\n")
        .filter((line) => line.startsWith("data:"))
        .map((line) => line.slice(5).replace(/^\s/, ""));
      if (!dataLines.length) continue;
      const payload = dataLines.join("\n");
      if (payload === "[DONE]") {
        onEvent({ done: true });
        return true;
      }
      try {
        onEvent({ data: JSON.parse(payload) });
      } catch {
        onEvent({ raw: payload });
      }
    }
  }
  return false;
}

async function streamSSE(path, body, extra = {}) {
  const started = performance.now();
  const method = "POST";
  let res;
  try {
    res = await fetch(path, {
      method,
      headers: {
        Accept: "text/event-stream",
        "Content-Type": "application/json",
      },
      body: JSON.stringify(body),
      signal: extra.signal,
    });
  } catch (err) {
    throw fetchError(err, method, path, started);
  }

  const headers = headerMap(res);
  const ctype = headers["content-type"] || "";
  if (!res.ok || ctype.includes("application/json")) {
    const text = await res.text();
    const data = decodeBody(text);
    const ms = elapsed(started);
    if (!res.ok) {
      throw new ApiError(apiMessage(data, res.status + " " + res.statusText), {
        status: res.status,
        data,
        raw: text,
        headers,
        ms,
        method,
        path,
      });
    }
    return {
      status: res.status,
      data,
      headers,
      ms,
      raw: text,
      streamed: false,
    };
  }

  let text = "";
  let finish = null;
  let usage = null;
  let id = "";
  let model = "";
  let done = false;
  try {
    done = await readSSE(res, (evt) => {
      if (evt.done) return;
      const data = evt.data;
      if (!data) return;
      if (data.id) id = data.id;
      if (data.model) model = data.model;
      if (data.usage) usage = data.usage;
      const choice = data.choices && data.choices[0];
      if (choice && choice.finish_reason) finish = choice.finish_reason;
      const delta = choice && choice.delta;
      if (delta && delta.content) text += delta.content;
      if (extra.onEvent)
        extra.onEvent({ text, data, finish, usage, id, model });
    });
  } catch (err) {
    throw fetchError(err, method, path, started);
  }

  const data = {
    id,
    object: "chat.completion",
    model,
    choices: [
      {
        index: 0,
        message: { role: "assistant", content: text },
        finish_reason: finish,
      },
    ],
    usage,
  };
  return {
    status: res.status,
    data,
    headers,
    ms: elapsed(started),
    raw: JSON.stringify(data),
    streamed: true,
    done,
    text,
  };
}

const idPath = (base, id) => base + "/" + encodeURIComponent(id);

export const Control = {
  service: () => api("GET", "/api"),
  accounts: () => api("GET", "/api/accounts"),
  account: (id) => api("GET", idPath("/api/accounts", id)),
  addAccount: (body) => api("POST", "/api/accounts", body),
  deleteAccount: (id) => api("DELETE", idPath("/api/accounts", id)),
  logins: () => api("GET", "/api/login"),
  login: (id) => api("GET", idPath("/api/login", id)),
  startLogin: () => api("POST", "/api/login"),
  deleteLogin: (id) => api("DELETE", idPath("/api/login", id)),
};

export const AI = {
  models: (extra) => api("GET", "/ai/v1/models", undefined, extra),
  chat: (body, extra) => api("POST", "/ai/v1/chat/completions", body, extra),
  completions: (body, extra) => api("POST", "/ai/v1/completions", body, extra),
  stream: (path, body, extra) => streamSSE(path, body, extra),
};
