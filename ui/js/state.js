import { Control } from "./api.js";
import { $, errText, setLive, toast } from "./lib.js";
import { parseHash, watchingLogins } from "./router.js";

export const state = {
  service: {},
  accounts: [],
  loginAttempts: [],
  account: null,
  attempt: null,
  connected: false,
  ready: false,
  view: "",
  error: null,
};

let inflight = null;
let pollTimer = 0;
let connectivitySig = null;

function noteLoginChanges(previous, next) {
  if (!state.ready) return;
  const was = new Map(previous.map((item) => [item.id, item.state]));
  for (const item of next) {
    if (was.get(item.id) !== "pending") continue;
    if (item.state === "succeeded")
      toast("login succeeded · account added", "info");
    else if (item.state === "failed")
      toast(item.error || "login failed", "error");
    else if (item.state === "expired") toast("login expired", "warn");
  }
}

function connectivitySignature(err) {
  if (!err) return "ok";
  const bits = [];
  if (err && err.network) bits.push("network");
  if (err && err.aborted) bits.push("aborted");
  if (err && typeof err.status !== "undefined")
    bits.push("status=" + err.status);
  bits.push(String(err && err.message ? err.message : err));
  return bits.join("|");
}

function notifyConnectivityIfChanged(nextConnected, err) {
  const sig = nextConnected ? "ok" : connectivitySignature(err);
  if (connectivitySig === null) {
    connectivitySig = sig;
    return;
  }
  if (sig === connectivitySig) return;
  connectivitySig = sig;
  if (nextConnected) toast("back online · /api/status", "info");
  else
    toast(
      err && err.network
        ? "offline · cannot reach /api/status"
        : errText(err, "offline"),
      err && err.network ? "warn" : "error",
    );
}

async function load() {
  const [svc, acc, log] = await Promise.all([
    Control.status(),
    Control.accounts(),
    Control.loginAttempts(),
  ]);
  const loginAttempts = (log.data && log.data.login_attempts) || [];
  noteLoginChanges(state.loginAttempts, loginAttempts);
  state.service = svc.data || {};
  state.accounts = (acc.data && acc.data.accounts) || [];
  state.loginAttempts = loginAttempts;
  state.connected = true;
  state.error = null;
  $("nav-accounts").textContent = String(state.accounts.length);
  $("nav-login").textContent = String(state.loginAttempts.length);
  setLive("ok", "live · /api/status");
  notifyConnectivityIfChanged(true);
  state.ready = true;
}

export function refreshAll({ silent = false } = {}) {
  if (inflight) return inflight;
  if (!silent) setLive("busy", "syncing");
  inflight = load()
    .catch((err) => {
      state.connected = false;
      state.error = err;
      setLive("bad", "offline");
      notifyConnectivityIfChanged(false, err);
      throw err;
    })
    .finally(() => {
      inflight = null;
    });
  return inflight;
}

export function stopPoll() {
  clearTimeout(pollTimer);
  pollTimer = 0;
}

export function syncPoll(onTick) {
  stopPoll();
  const route = parseHash();
  const pending = state.loginAttempts.some((item) => item.state === "pending");
  if (!state.connected || !watchingLogins(route) || !pending) return;
  pollTimer = setTimeout(async () => {
    try {
      await refreshAll({ silent: true });
    } catch {
      // onTick paints offline; show() restarts poll only after a live render
    }
    await onTick();
  }, 2000);
}

export function pendingCount() {
  return state.loginAttempts.filter((item) => item.state === "pending").length;
}

export function sortedLogins() {
  const order = { pending: 0, succeeded: 1, failed: 2, expired: 3 };
  return state.loginAttempts
    .slice()
    .sort((a, b) => (order[a.state] ?? 9) - (order[b.state] ?? 9));
}

export function findAccount(id) {
  return state.accounts.find((item) => item.id === id) || null;
}

export function findLogin(id) {
  return state.loginAttempts.find((item) => item.id === id) || null;
}
