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

/** The refresh and the browser-side evaluation. It only ever writes with `textContent`. */
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

const status = document.querySelector('[data-testid="evaluate-status"]');
const result = document.querySelector('[data-testid="evaluate-result"]');
document.querySelector('[data-testid="evaluate-button"]').addEventListener("click", async () => {
  status.textContent = "...";
  try {
    const response = await fetch(config.serverUrl + "/sdk/v1/evaluate", {
      method: "POST",
      headers: { Authorization: "Bearer " + config.clientKey, "Content-Type": "application/json" },
      body: JSON.stringify({ context: config.user }),
    });
    result.textContent = JSON.stringify(await response.json(), null, 2);
    status.textContent = String(response.status);
  } catch (error) {
    result.textContent = String(error);
    status.textContent = "error";
  }
});
`;

/** Keeps `?flag=` through the shopper picker, which is a GET form. */
function extraFlagInput(flag: string | null): string {
  return flag === null ? "" : `<input type="hidden" name="flag" value="${escapeHtml(flag)}">`;
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

  const statusRows = [
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
<title>Shop checkout: a FeatureSlash example</title>
</head>
<body>
<header>
<h1>Shop checkout</h1>
<p>A fictional shop whose checkout is steered by FeatureSlash flags. Change a flag in the dashboard or the CLI and the values below follow within a second.</p>
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
${statusRows}
</dl>
</section>
<section aria-labelledby="browser">
<h2 id="browser">Evaluate in the browser</h2>
<p>Asks the server for this shopper's flags with a client key, from this page.</p>
<button type="button" data-testid="evaluate-button">Evaluate in the browser</button>
<p>Answer: <output data-testid="evaluate-status">-</output></p>
<pre data-testid="evaluate-result"></pre>
</section>
</main>
<script type="application/json" id="config">${data}</script>
<script>${SCRIPT}</script>
</body>
</html>
`;
}
