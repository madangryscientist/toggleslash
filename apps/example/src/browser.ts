/// <reference lib="dom" />
import { createBrowserClient } from "@hackcove/toggleslash-sdk-browser";
import type { SingleContext } from "@hackcove/toggleslash-sdk-browser";

/** What the page writes into its `#config` element (`src/page.ts`). */
interface PageConfig {
  serverUrl: string;
  /** A client key: the only key the page and this bundle ever hold. */
  clientKey: string;
  user: SingleContext;
  /** The user's context signed by the example's server, when its environment is in secure mode. */
  signature?: string;
}

const element = document.getElementById("config");
if (element === null) throw new Error("the page has no #config element");
const config: PageConfig = JSON.parse(element.textContent ?? "");

const client = createBrowserClient({
  url: config.serverUrl,
  clientKey: config.clientKey,
  context: config.user,
  ...(config.signature === undefined ? {} : { signature: config.signature }),
});

/** Writes the cache into every cell the page marked with a flag and a field. It only uses `textContent`. */
function paint(): void {
  for (const cell of document.querySelectorAll<HTMLElement>("[data-browser-flag]")) {
    const { value, reason } = client.detail(cell.dataset["browserFlag"] ?? "", null);
    cell.textContent =
      cell.dataset["field"] === "reason" ? reason.kind : (JSON.stringify(value) ?? "null");
  }
}

void client.ready().then(paint);
client.on("change", paint);
