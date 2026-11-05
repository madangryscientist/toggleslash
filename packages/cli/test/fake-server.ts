import type { Fetch } from "../src/api.ts";

export interface Seen {
  method: string;
  path: string;
  query: string;
  headers: Record<string, string>;
  body: unknown;
}

export type Handler = (seen: Seen) => Response | undefined;

export function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

export function apiError(status: number, code: string, message: string): Response {
  return json(status, { error: { code, message } });
}

/**
 * A fake admin API: `routes` maps `METHOD /path` to a handler, an unknown route is a 404 in the server's own
 * error shape, and every request is recorded. No socket is opened.
 */
export function fakeServer(routes: Readonly<Record<string, Handler | Response>>): {
  fetch: Fetch;
  seen: Seen[];
} {
  const seen: Seen[] = [];
  const doFetch: Fetch = async (input, init) => {
    const url = new URL(input);
    const headers: Record<string, string> = {};
    new Headers(init.headers).forEach((value, name) => {
      headers[name] = value;
    });
    const request: Seen = {
      method: init.method ?? "GET",
      path: url.pathname,
      query: url.search,
      headers,
      body: typeof init.body === "string" ? JSON.parse(init.body) : undefined,
    };
    seen.push(request);
    const route = routes[`${request.method} ${request.path}`];
    if (route === undefined) return apiError(404, "not_found", "Not Found");
    if (typeof route !== "function") {
      // A fresh Response each time: a body can be read once.
      return new Response(route.status === 204 ? null : await route.clone().text(), {
        status: route.status,
        headers: route.headers,
      });
    }
    return route(request) ?? apiError(500, "internal", "handler returned nothing");
  };
  return { fetch: doFetch, seen };
}
