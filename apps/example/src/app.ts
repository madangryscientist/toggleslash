import type {
  ClientStatus,
  Context,
  DetailReason,
  VariationDetail,
} from "@hackcove/featureslash-sdk";
import { FLAGS } from "./flags.ts";
import { renderPage } from "./page.ts";
import { USERS, userFor } from "./users.ts";

/** The two things the app uses from the SDK (a `Client` fits), so a test can stand a fake in. */
export interface FlagClient {
  variationDetail(flagKey: string, context: Context, fallback: unknown): VariationDetail<unknown>;
  status(): ClientStatus;
}

interface FlagState {
  key: string;
  value: unknown;
  variationIndex: number | null;
  reason: DetailReason;
}

export interface State {
  user: Context;
  flags: FlagState[];
  status: ClientStatus;
}

interface AppOptions {
  client: FlagClient;
  /** The server's base URL as the browser reaches it, for the page's "evaluate in the browser" button. */
  serverUrl: string;
  /** A client key: it may be sent to the browser. The server key is never given to this module. */
  clientKey: string;
}

/** What the shop knows for one user, from the SDK's memory: no request is made. */
function stateFor(client: FlagClient, user: Context): State {
  const flags = FLAGS.map(({ key, fallback }) => {
    const { value, variationIndex, reason } = client.variationDetail(key, user, fallback);
    return { key, value, variationIndex, reason };
  });
  return { user, flags, status: client.status() };
}

function json(body: unknown, status: number): Response {
  return Response.json(body, { status, headers: { "Cache-Control": "no-store" } });
}

/** The app as a `fetch` handler: `GET /` is the page and `GET /state` the page's data. */
export function createApp(options: AppOptions): (request: Request) => Response {
  return (request) => {
    const url = new URL(request.url);
    if (url.pathname !== "/" && url.pathname !== "/state") {
      return json({ error: "not found" }, 404);
    }
    if (request.method !== "GET") return json({ error: "method not allowed" }, 405);

    const user = userFor(url.searchParams.get("user"));
    if (user === null) {
      return json({ error: `unknown user; try one of ${USERS.map((u) => u.key).join(", ")}` }, 400);
    }
    const state = stateFor(options.client, user);
    if (url.pathname === "/state") return json(state, 200);

    const html = renderPage(state, { serverUrl: options.serverUrl, clientKey: options.clientKey });
    return new Response(html, {
      headers: {
        "Content-Type": "text/html; charset=utf-8",
        "Cache-Control": "no-store",
        "X-Content-Type-Options": "nosniff",
      },
    });
  };
}
