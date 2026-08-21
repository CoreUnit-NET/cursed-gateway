import { errText, html, mountView, navActive, navKey } from "./lib.js";
import { state } from "./state.js";
import { renderAccounts } from "./views/accounts.js";
import { renderAI } from "./views/ai.js";
import { renderLogin } from "./views/login.js";
import { renderOverview } from "./views/overview.js";

export function renderOffline(page, error) {
  navActive("overview");
  mountView(
    page,
    "offline",
    html` <div class="head">
      <div>
        <h2>Waiting for /api/status</h2>
        <p class="err">
          ${errText(error, "not connected")}. Serve this page on the same origin
          as cursed-gateway (or proxy <code>/api/*</code> here). There is no UI
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
  navActive(navKey(route));
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

export { closeLogin, createLoginAttempt } from "./views/login.js";
export { removeAccount } from "./views/accounts.js";
export { cancelAITest, copyAIJson, listModels } from "./views/ai.js";
