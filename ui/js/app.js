import { $, copyText, errText, toast } from "./lib.js";
import { parseHash, routeKey } from "./router.js";
import { refreshAll, state, stopPoll, syncPoll } from "./state.js";
import {
  cancelAITest,
  closeLogin,
  copyAIJson,
  listModels,
  removeAccount,
  render,
  renderOffline,
  startLogin,
} from "./views.js";

const page = $("page");
let seq = 0;
let retryTimer = 0;

function stopRetry() {
  clearTimeout(retryTimer);
  retryTimer = 0;
}

function startRetry() {
  stopRetry();
  retryTimer = setTimeout(() => show({ retry: true }), 4000);
}

async function show({ patch = false, retry = false } = {}) {
  const my = ++seq;
  try {
    if (retry || !state.ready) await refreshAll({ silent: retry });
    if (my !== seq) return;
    if (!state.connected) throw state.error || new Error("not connected");
    stopRetry();
    const route = parseHash();
    await render(page, route, {
      patch: patch && state.view === routeKey(route),
      stale: () => my !== seq,
    });
    if (my !== seq) return;
    state.view = routeKey(route);
    syncPoll(() => show({ patch: true }));
  } catch (error) {
    if (my !== seq) return;
    stopPoll();
    renderOffline(page, error);
    startRetry();
  }
}

document.addEventListener("click", async (event) => {
  const start = event.target.closest("#start-login");
  const delAccount = event.target.closest("[data-del-account]");
  const delLogin = event.target.closest("[data-del-login]");
  const copy = event.target.closest("[data-copy]");
  const action = event.target.closest("[data-action]");

  try {
    if (start) {
      event.preventDefault();
      await startLogin();
      return;
    }
    if (action?.dataset.action === "list-models") {
      event.preventDefault();
      await listModels();
      return;
    }
    if (action?.dataset.action === "cancel-ai-test") {
      event.preventDefault();
      cancelAITest();
      return;
    }
    if (action?.dataset.action === "copy-ai-json") {
      event.preventDefault();
      copyAIJson(action.dataset.aiJson);
      return;
    }
    if (delAccount) {
      event.preventDefault();
      event.stopPropagation();
      await removeAccount(delAccount.dataset.delAccount);
      return;
    }
    if (delLogin) {
      event.preventDefault();
      event.stopPropagation();
      await closeLogin(delLogin.dataset.delLogin);
      return;
    }
    if (copy) {
      event.preventDefault();
      event.stopPropagation();
      copyText(copy.dataset.copy);
      return;
    }
    if (
      action?.dataset.action === "retry" ||
      action?.dataset.action === "refresh"
    ) {
      event.preventDefault();
      stopRetry();
      try {
        await refreshAll();
        await show({ patch: true });
      } catch (error) {
        renderOffline(page, error);
        startRetry();
        throw error;
      }
    }
  } catch (error) {
    toast(errText(error), true);
  }
});

window.addEventListener("hashchange", () => {
  stopPoll();
  show();
});

refreshAll()
  .then(() => show())
  .catch((error) => {
    renderOffline(page, error);
    startRetry();
  });
