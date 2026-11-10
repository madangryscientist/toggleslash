import type { State } from "./app.ts";
import { USERS } from "./users.ts";

interface PageConfig {
  serverUrl: string;
  /** A client key. The only key that is ever written into the page. */
  clientKey: string;
  /** The `?flag=` the page was asked for, kept so its refresh asks for it too. */
  extraFlag: string | null;
}

function escapeHtml(text: string): string {
  return text
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

/** How a value reads on the page; the script below has the same two formats. */
function display(value: unknown, format: "json" | "plain"): string {
  if (format === "json") return JSON.stringify(value) ?? "null";
  if (value === null || value === undefined) return "-";
  return typeof value === "string" ? value : JSON.stringify(value);
}

/** An element the script repaints: `path` finds its value in the `/state` document. */
function cell(testid: string, state: State, path: string, format: "json" | "plain") {
  const value = path.split(".").reduce<unknown>((into, step) => {
    if (typeof into !== "object" || into === null) return undefined;
    return Object.fromEntries(Object.entries(into))[step];
  }, state);
  return `<code data-testid="${escapeHtml(testid)}" data-path="${escapeHtml(path)}" data-format="${format}">${escapeHtml(display(value, format))}</code>`;
}

/** The refresh of the server-side table. It only ever writes with `textContent`. */
const SCRIPT = `
const config = JSON.parse(document.getElementById("config").textContent);
function display(value, format) {
  if (format === "json") return JSON.stringify(value);
  if (value === null || value === undefined) return "-";
  return typeof value === "string" ? value : JSON.stringify(value);
}
function paint(state) {
  for (const el of document.querySelectorAll("[data-path]")) {
    const value = el.dataset.path.split(".").reduce((into, step) => (into == null ? undefined : into[step]), state);
    el.textContent = display(value, el.dataset.format);
  }
}
async function refresh() {
  try {
    const query = "?user=" + encodeURIComponent(config.user.key) + (config.flag === null ? "" : "&flag=" + encodeURIComponent(config.flag));
    const response = await fetch("/state" + query, { cache: "no-store" });
    if (response.ok) paint(await response.json());
  } catch {
    // The next tick tries again.
  }
}
setInterval(refresh, 1000);
`;

/** Keeps `?flag=` through the shopper picker, which is a GET form. */
function extraFlagInput(flag: string | null): string {
  return flag === null ? "" : `<input type="hidden" name="flag" value="${escapeHtml(flag)}">`;
}

/** The rows `src/browser.ts` repaints (it paints whichever `data-browser-flag` cells it finds): `new-checkout` and the `?flag=` flag. */
function browserRows(extraFlag: string | null): string {
  const keys = new Set(["new-checkout", ...(extraFlag === null ? [] : [extraFlag])]);
  return [...keys]
    .map(
      (key) => `<tr>
<th scope="row">${escapeHtml(key)}</th>
<td><code data-testid="browser-flag-${escapeHtml(key)}-value" data-browser-flag="${escapeHtml(key)}" data-field="value">-</code></td>
<td><code data-testid="browser-flag-${escapeHtml(key)}-reason" data-browser-flag="${escapeHtml(key)}" data-field="reason">-</code></td>
</tr>`,
    )
    .join("\n");
}

/** The SDK status as definition-list entries, each a repainted cell. */
function statusRows(state: State): string {
  return [
    ["etag", "ETag of the ruleset held", "status.etag"],
    ["last-sync-at", "Last confirmed (epoch ms)", "status.lastSyncAt"],
    ["stream", "Stream", "status.stream"],
    ["last-error", "Last error", "status.lastError"],
  ]
    .map(
      ([name, label, path]) =>
        `<dt>${escapeHtml(label ?? "")}</dt><dd>${cell(`status-${name}`, state, path ?? "", "plain")}</dd>`,
    )
    .join("\n");
}

export function renderPage(state: State, config: PageConfig): string {
  const plan = state.user.attributes?.["plan"];
  const options = USERS.map((user) => {
    const selected = user.key === state.user.key ? " selected" : "";
    return `<option value="${escapeHtml(user.key)}"${selected}>${escapeHtml(user.key)}</option>`;
  }).join("");
  const rows = state.flags
    .map(
      (flag, i) => `<tr>
<th scope="row">${escapeHtml(flag.key)}</th>
<td>${cell(`flag-${flag.key}-value`, state, `flags.${i}.value`, "json")}</td>
<td>${cell(`flag-${flag.key}-reason`, state, `flags.${i}.reason.kind`, "plain")}</td>
</tr>`,
    )
    .join("\n");

  // `<` is escaped so no value in the document can close the script element.
  const { serverUrl, clientKey, extraFlag: flag } = config;
  const data = JSON.stringify({ user: state.user, serverUrl, clientKey, flag }).replaceAll(
    "<",
    "\\u003c",
  );

  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Shop checkout: a ToggleSlash example</title>
</head>
<body>
<header>
<h1>Shop checkout</h1>
<p>A fictional shop whose checkout is steered by ToggleSlash flags. Change a flag in the dashboard or the CLI and the values below follow within a second.</p>
</header>
<main>
<form method="get" action="/">
${extraFlagInput(config.extraFlag)}
<label for="user">Shopper</label>
<select id="user" name="user" data-testid="user-select">${options}</select>
<button type="submit" data-testid="user-submit">Switch</button>
</form>
<p>Viewing as <strong data-testid="current-user">${escapeHtml(state.user.key)}</strong>, plan <span data-testid="current-plan">${escapeHtml(typeof plan === "string" ? plan : "-")}</span>.</p>
<section aria-labelledby="flags">
<h2 id="flags">Flags for this shopper</h2>
<table>
<thead><tr><th scope="col">Flag</th><th scope="col">Value</th><th scope="col">Reason</th></tr></thead>
<tbody>
${rows}
</tbody>
</table>
</section>
<section aria-labelledby="sdk">
<h2 id="sdk">SDK status</h2>
<dl>
${statusRows(state)}
</dl>
</section>
<section aria-labelledby="browser">
<h2 id="browser">In the browser (client key)</h2>
<p>The same flags, evaluated by the server for this shopper through the browser SDK and kept current by the change signal.</p>
<table>
<thead><tr><th scope="col">Flag</th><th scope="col">Value</th><th scope="col">Reason</th></tr></thead>
<tbody>
${browserRows(config.extraFlag)}
</tbody>
</table>
</section>
</main>
<script type="application/json" id="config">${data}</script>
<script>${SCRIPT}</script>
<script type="module" src="/browser.js"></script>
</body>
</html>
`;
}
