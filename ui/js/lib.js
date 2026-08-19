export const $ = (id) => document.getElementById(id);

export function esc(value) {
  return String(value ?? "").replace(
    /[&<>"']/g,
    (ch) =>
      ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&#39;",
      })[ch],
  );
}

export function raw(value) {
  return { __html: String(value ?? "") };
}

export function when(cond, markup) {
  return cond ? raw(typeof markup === "function" ? markup() : markup) : "";
}

export function join(items, fn) {
  return raw((items || []).map(fn).join(""));
}

export function html(strings, ...values) {
  let out = strings[0];
  for (let i = 0; i < values.length; i++) {
    const value = values[i];
    out +=
      value && Object.prototype.hasOwnProperty.call(value, "__html")
        ? value.__html
        : esc(value);
    out += strings[i + 1];
  }
  return out;
}

export function badge(kind, label) {
  const key = kind || "unknown";
  return raw(html`<span class="badge ${key}">${label || key}</span>`);
}

export function fmtTime(ms) {
  if (!ms) return "—";
  const date = new Date(Number(ms));
  if (Number.isNaN(date.getTime())) return String(ms);
  const diff = date.getTime() - Date.now();
  const abs = Math.abs(diff);
  const mins = Math.round(abs / 60000);
  const hours = Math.round(abs / 3600000);
  const days = Math.round(abs / 86400000);
  const rel = mins < 90 ? `${mins}m` : hours < 48 ? `${hours}h` : `${days}d`;
  return `${diff < 0 ? `expired ${rel} ago` : `in ${rel}`} · ${date.toLocaleString()}`;
}

export function errText(error, fallback = "request failed") {
  const message = error && error.message;
  return message ? String(message) : fallback;
}

export function toast(message, kindOrBad = "info") {
  // Backward compatible: historically `toast(message, true)` meant "bad".
  let kind = "info";
  if (typeof kindOrBad === "boolean") kind = kindOrBad ? "error" : "info";
  else if (kindOrBad) kind = String(kindOrBad);

  const el = document.createElement("div");
  el.className = "toast" + (kind === "error" ? " bad" : "") + " " + kind;
  el.setAttribute("role", kind === "error" ? "alert" : "status");
  el.textContent = message;
  $("toasts").appendChild(el);
  setTimeout(() => el.remove(), 4200);
}

export function confirmDialog(text) {
  const dialog = $("dialog");
  dialog.innerHTML = html` <form class="dlg" method="dialog">
    <h3 id="dlg-title">Confirm</h3>
    <p>${text}</p>
    <div class="actions">
      <button class="btn ghost" type="submit" value="cancel" autofocus>
        Cancel
      </button>
      <button class="btn danger" type="submit" value="ok">Confirm</button>
    </div>
  </form>`;
  return new Promise((resolve) => {
    const done = () => {
      dialog.removeEventListener("close", done);
      resolve(dialog.returnValue === "ok");
    };
    dialog.addEventListener("close", done);
    // Escape leaves the previous returnValue in place; default to cancel.
    dialog.returnValue = "cancel";
    dialog.showModal();
    dialog.querySelector("[value=cancel]")?.focus();
  });
}

export async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    toast("copied");
  } catch {
    toast("copy failed", true);
  }
}

export function setLive(status, label) {
  const dot = $("dot");
  const text = $("live-label");
  if (dot) dot.className = "dot " + status;
  if (text) text.textContent = label;
}

export function navActive(tab) {
  document.querySelectorAll(".tabs a").forEach((link) => {
    link.classList.toggle("active", link.dataset.nav === tab);
  });
}

export function setModes(items, active) {
  const nav = $("modes");
  if (!items?.length) {
    nav.hidden = true;
    nav.innerHTML = "";
    return;
  }
  nav.hidden = false;
  nav.innerHTML = items
    .map(
      (item) => html`
        <a href="${item.href}" class="${item.id === active ? "active" : ""}"
          >${item.label}</a
        >
      `,
    )
    .join("");
}

export function go(hash) {
  const next = hash.startsWith("#") ? hash : "#" + hash;
  if (location.hash === next) {
    window.dispatchEvent(new Event("hashchange"));
    return;
  }
  location.hash = next;
}

export async function withBusy(el, fn) {
  if (!el || el.disabled) return;
  el.disabled = true;
  try {
    await fn();
  } finally {
    el.disabled = false;
  }
}

export function fillSlot(root, name, markup) {
  const slot = root.querySelector(`[data-slot="${name}"]`);
  if (slot) slot.innerHTML = markup;
}

export function sameView(page, signature) {
  return page.dataset.view === signature;
}

export function mountView(page, signature, markup) {
  page.dataset.view = signature;
  page.innerHTML = markup;
}
