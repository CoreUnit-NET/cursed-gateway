import { html, raw, when } from "../lib.js";

export function empty(text, href, action) {
  return raw(
    html`<div class="empty">
      ${text}${when(href, () => html` <a href="${href}">${action}</a>`)}
    </div>`,
  );
}
