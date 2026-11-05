/**
 * The HTTP half of the admin API client: one authenticated call, its failures, and the paths. Runs only in
 * the Worker; the `.server.ts` name makes TanStack Start's import protection fail the build if a client
 * module ever imports it.
 */
import { isRecord, type Json } from "./json.ts";

/** `fetch`, narrowed to what the client calls, so a test can pass a fake server. */
export type Fetch = (input: string, init: RequestInit) => Promise<Response>;

export interface Connection {
  url: string;
  token: string;
}

/** A call that did not give the dashboard what it needed. `kind` picks the page; `message` never has the token. */
export class ApiFailure extends Error {
  readonly kind: "unreachable" | "unauthorized" | "error";
  readonly status: number | undefined;
  /** The server's own message without its code (`rules[0].clauses must...`): what a field-level error is read from. */
  readonly detail: string | undefined;
  constructor(
    kind: "unreachable" | "unauthorized" | "error",
    message: string,
    status?: number,
    detail?: string,
  ) {
    super(message);
    this.name = "ApiFailure";
    this.kind = kind;
    this.status = status;
    this.detail = detail;
  }
}

const TIMEOUT_MS = 10_000;

/** `/api/v1/projects/p/flags`: each segment is encoded, so a key can never climb out of its place in the path. */
export function apiPath(...segments: string[]): string {
  return `/api/v1/${segments.map(encodeURIComponent).join("/")}`;
}

/** The server's `{ "error": { "code", "message" } }`, or nothing when the body is not that. */
function errorParts(text: string): { code: string | undefined; message: string } | undefined {
  try {
    const parsed: unknown = JSON.parse(text);
    const error = isRecord(parsed) ? parsed["error"] : undefined;
    if (isRecord(error) && typeof error["message"] === "string") {
      return {
        code: typeof error["code"] === "string" ? error["code"] : undefined,
        message: error["message"],
      };
    }
  } catch {
    // Not JSON: the status line is all there is to say.
  }
  return undefined;
}

/** The error as `code: message`. */
function errorDetail(text: string): string | undefined {
  const parts = errorParts(text);
  if (parts === undefined) return undefined;
  return parts.code === undefined ? parts.message : `${parts.code}: ${parts.message}`;
}

interface Answer {
  status: number;
  text: string;
}

interface Outgoing {
  method: string;
  path: string;
  body: unknown;
}

async function send(
  connection: Connection,
  doFetch: Fetch,
  { method, path, body }: Outgoing,
): Promise<Answer> {
  try {
    const response = await doFetch(`${connection.url}${path}`, {
      method,
      headers: {
        authorization: `Bearer ${connection.token}`,
        accept: "application/json",
        ...(body === undefined ? {} : { "content-type": "application/json" }),
      },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      // A redirect could carry the token somewhere else; the server does not redirect.
      redirect: "manual",
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
    return { status: response.status, text: await response.text() };
  } catch (error) {
    const why = error instanceof Error ? error.message : "unknown error";
    throw new ApiFailure("unreachable", `Could not reach ${connection.url}: ${why}`);
  }
}

/** Throws the failure a non-2xx answer stands for. */
function check({ status, text }: Answer): void {
  if (status === 401 || status === 403) {
    throw new ApiFailure(
      "unauthorized",
      `The server rejected the admin token (${status}).`,
      status,
    );
  }
  const detail = errorDetail(text);
  if (status >= 500 && detail === undefined) {
    throw new ApiFailure("unreachable", `The server answered ${status}.`, status);
  }
  if (status < 200 || status >= 300) {
    throw new ApiFailure(
      "error",
      detail ?? `The server answered ${status}.`,
      status,
      errorParts(text)?.message,
    );
  }
}

function parseBody({ status, text }: Answer): unknown {
  if (text === "") return null;
  try {
    return JSON.parse(text);
  } catch {
    throw new ApiFailure("error", `The server answered ${status} with a body that is not JSON.`);
  }
}

export type Call = (method: string, path: string, body?: unknown) => Promise<unknown>;

export async function list(call: Call, path: string): Promise<Json[]> {
  const body = await call("GET", path);
  if (!Array.isArray(body)) throw new ApiFailure("error", "The server's answer was not a list.");
  return body.filter(isRecord);
}

/** One authenticated call to the admin API: the parsed body, or an {@link ApiFailure}. */
export function makeCall(connection: Connection, doFetch: Fetch): Call {
  return async (method, path, body) => {
    const answer = await send(connection, doFetch, { method, path, body });
    check(answer);
    return parseBody(answer);
  };
}
