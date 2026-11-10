import { FailureError } from "./errors.ts";
import type { Connection } from "./config.ts";
import { isObject } from "./render.ts";

/** `fetch`, narrowed to what the CLI calls, so a test can pass a fake server. */
export type Fetch = (input: string, init: RequestInit) => Promise<Response>;

/** The server's JSON, which the CLI shows and does not interpret. */
export type Json = unknown;

export interface Api {
  get: (path: string, query?: Query) => Promise<Json>;
  post: (path: string, body?: Json) => Promise<Json>;
  patch: (path: string, body?: Json) => Promise<Json>;
  put: (path: string, body?: Json) => Promise<Json>;
  delete: (path: string) => Promise<Json>;
}

/** Query parameters; `undefined` ones are left out. */
export type Query = Readonly<Record<string, string | undefined>>;

const TIMEOUT_MS = 30_000;

/** `/api/v1/projects/p/flags/f`: each segment is encoded, so a key can never climb out of its place in the path. */
export function apiPath(...segments: string[]): string {
  return `/api/v1/${segments.map(encodeURIComponent).join("/")}`;
}

/** The server's `{ "error": { "code", "message" } }`, as `code: message`, or nothing when the body is not that. */
function errorDetail(text: string): string | undefined {
  try {
    const parsed: unknown = JSON.parse(text);
    const error = isObject(parsed) ? parsed["error"] : undefined;
    if (
      isObject(error) &&
      typeof error["code"] === "string" &&
      typeof error["message"] === "string"
    ) {
      return `${error["code"]}: ${error["message"]}`;
    }
  } catch {
    // Not JSON: the status line is all there is to say.
  }
  return undefined;
}

function queryString(query: Query | undefined): string {
  const search = new URLSearchParams();
  for (const [name, value] of Object.entries(query ?? {})) {
    if (value !== undefined) search.set(name, value);
  }
  return search.size > 0 ? `?${search.toString()}` : "";
}

function interpret(response: Response, text: string): Json {
  if (response.status === 401) {
    throw new FailureError(
      "the server rejected the admin token (401). Check TOGGLESLASH_TOKEN or run `toggleslash config set-token`.",
    );
  }
  if (!response.ok) {
    const detail = errorDetail(text);
    throw new FailureError(
      `the server answered ${response.status}${detail === undefined ? "" : ` ${detail}`}`,
    );
  }
  if (text === "") return null;
  try {
    return JSON.parse(text);
  } catch {
    throw new FailureError(`the server answered ${response.status} with a body that is not JSON.`);
  }
}

/**
 * The admin API over an injected `fetch`. Every failure is a `FailureError` (exit 1) whose message has the
 * status and the server's own error and never the request, so the token cannot be in it.
 */
export function createApi(connection: Connection, doFetch: Fetch): Api {
  async function send(method: string, path: string, query: Query | undefined, body: Json) {
    const headers: Record<string, string> = {
      authorization: `Bearer ${connection.token}`,
      accept: "application/json",
    };
    if (body !== undefined) headers["content-type"] = "application/json";
    try {
      const response = await doFetch(`${connection.url}${path}${queryString(query)}`, {
        method,
        headers,
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
        // A redirect could carry the token somewhere else; the server does not redirect, so treat one as a failure.
        redirect: "manual",
        signal: AbortSignal.timeout(TIMEOUT_MS),
      });
      return { response, text: await response.text() };
    } catch (error) {
      const why = error instanceof Error ? error.message : "unknown error";
      throw new FailureError(`could not reach ${connection.url}: ${why}`);
    }
  }

  async function call(
    method: string,
    path: string,
    query: Query | undefined,
    body: Json,
  ): Promise<Json> {
    const { response, text } = await send(method, path, query, body);
    return interpret(response, text);
  }

  return {
    get: (path, query) => call("GET", path, query, undefined),
    post: (path, body) => call("POST", path, undefined, body),
    patch: (path, body) => call("PATCH", path, undefined, body),
    put: (path, body) => call("PUT", path, undefined, body),
    delete: (path) => call("DELETE", path, undefined, undefined),
  };
}
