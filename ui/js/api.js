export class ApiError extends Error {
  constructor(message, extra = {}) {
    super(message);
    this.name = "ApiError";
    Object.assign(this, extra);
  }
}

export async function api(method, path, body) {
  const opts = { method, headers: { Accept: "application/json" } };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = typeof body === "string" ? body : JSON.stringify(body);
  }

  let res;
  try {
    res = await fetch(path, opts);
  } catch (err) {
    throw new ApiError("cannot reach /api (" + err.message + ")", {
      network: true,
    });
  }

  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = { error: text };
    }
  }
  if (!res.ok) {
    throw new ApiError(
      (data && (data.error || data.message)) ||
        res.status + " " + res.statusText,
      {
        status: res.status,
        data,
      },
    );
  }
  return { status: res.status, data };
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
