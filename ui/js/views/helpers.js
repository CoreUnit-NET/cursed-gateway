import { html, when } from "../lib.js";

export function empty(text, href, action) {
  return html`<div class="empty">
    ${text}${when(href, () => html` <a href="${href}">${action}</a>`)}
  </div>`;
}
