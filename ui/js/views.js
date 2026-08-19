import { errText, html, mountView, navActive, setModes } from "./lib.js";
import { state } from "./state.js";
import { renderAccounts } from "./views/accounts.js";
import { renderAI } from "./views/ai.js";
import { renderLogin } from "./views/login.js";
import { renderOverview } from "./views/overview.js";

function modesFor(route) {
  if (route.tab === "accounts") {
    const items = [
      { id: "pool", href: "#/accounts", label: "Pool" },
      { id: "add", href: "#/accounts/add", label: "Add tokens" },
      { id: "import", href: "#/accounts/import", label: "Import JSON" },
    ];
    if (route.mode === "detail")
      items.push({ id: "detail", href: location.hash, label: "Account" });
    return items;
  }
  if (route.tab === "login") {
    const items = [
      { id: "attempts", href: "#/login", label: "Attempts" },
      { id: "start", href: "#/login/start", label: "Start login" },
    ];
    if (route.mode === "detail")
      items.push({ id: "detail", href: location.hash, label: "Attempt" });
    return items;
  }
  if (route.tab === "ai") {
    return [
      { id: "models", href: "#/ai", label: "Models" },
      { id: "test", href: "#/ai/test", label: "Test" },
    ];
  }
  return [];
}

export function renderOffline(page, error) {
  navActive("overview");
  setModes([]);
  mountView(
    page,
    "offline",
    html` <div class="head">
      <div>
        <h2>Waiting for /api</h2>
        <p class="err">
          ${errText(error, "not connected")}. Serve this page on the same origin
          as cursed-gateway (or proxy <code>/api</code> here). There is no UI
          auth and no remote origin field.
        </p>
      </div>
      <button class="btn primary" type="button" data-action="retry">
        Retry
      </button>
    </div>`,
  );
}

export async function render(
  page,
  route,
  { patch = false, stale = () => false } = {},
) {
  navActive(route.tab);
  setModes(modesFor(route), route.mode === "home" ? "" : route.mode);
  if (!state.connected) {
    renderOffline(page, state.error || new Error("not connected"));
    return;
  }
  if (route.tab === "accounts")
    return renderAccounts(page, route, { patch, stale });
  if (route.tab === "login") return renderLogin(page, route, { patch, stale });
  if (route.tab === "ai") return renderAI(page, route, { patch, stale });
  renderOverview(page, { patch });
}

export { closeLogin, startLogin } from "./views/login.js";
export { removeAccount } from "./views/accounts.js";
export { cancelAITest, copyAIJson, listModels } from "./views/ai.js";
