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

/** The shape of a flag key (the server's rule), so `?flag=` cannot carry anything else into the page. */
const FLAG_KEY = /^[a-z0-9][a-z0-9_-]{0,63}$/;

/**
 * What the shop knows for one user, from the SDK's memory: no request is made. `extraFlag` is one
 * more flag to read, with a `null` fallback, for a flag the shop does not know yet.
 */
function stateFor(client: FlagClient, user: Context, extraFlag: string | null): State {
  const known = extraFlag === null || FLAGS.some(({ key }) => key === extraFlag);
  const read = known ? FLAGS : [...FLAGS, { key: extraFlag, fallback: null }];
  const flags = read.map(({ key, fallback }) => {
    const { value, variationIndex, reason } = client.variationDetail(key, user, fallback);
    return { key, value, variationIndex, reason };
  });
  return { user, flags, status: client.status() };
}

function json(body: unknown, status: number): Response {
  return Response.json(body, { status, headers: { "Cache-Control": "no-store" } });
}

/** The app as a `fetch` handler: `GET /` is the page and `GET /state` the page's data; both take `?user=` and `?flag=`. */
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
    const extraFlag = url.searchParams.get("flag");
    if (extraFlag !== null && !FLAG_KEY.test(extraFlag)) {
      return json(
        {
          error:
            "flag must be a flag key: lowercase letters, digits, - and _, starting with a letter or digit",
        },
        400,
      );
    }
    const state = stateFor(options.client, user, extraFlag);
    if (url.pathname === "/state") return json(state, 200);

    const html = renderPage(state, {
      serverUrl: options.serverUrl,
      clientKey: options.clientKey,
      extraFlag,
    });
    return new Response(html, {
      headers: {
        "Content-Type": "text/html; charset=utf-8",
        "Cache-Control": "no-store",
        "X-Content-Type-Options": "nosniff",
      },
    });
  };
}
