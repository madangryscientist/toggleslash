/**
 * The typed client for the C# server's admin API. It runs only in the Worker (server functions), with
 * the token it is given, and nothing here is imported by a route component. The `.server.ts` name
 * makes TanStack Start's import protection fail the build if a client module ever imports it.
 */
import { isRecord, str, strings, type Json } from "./json.ts";
import type { Environment, FlagRow, NewFlag, Project } from "./types.ts";

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
  constructor(kind: "unreachable" | "unauthorized" | "error", message: string, status?: number) {
    super(message);
    this.name = "ApiFailure";
    this.kind = kind;
    this.status = status;
  }
}

const TIMEOUT_MS = 10_000;
/** How many per-flag config reads run at once: the list is N+1 until the server has a bulk read. */
const CONFIG_CONCURRENCY = 8;

/** `/api/v1/projects/p/flags`: each segment is encoded, so a key can never climb out of its place in the path. */
export function apiPath(...segments: string[]): string {
  return `/api/v1/${segments.map(encodeURIComponent).join("/")}`;
}

/** The server's `{ "error": { "code", "message" } }` as `code: message`, or nothing when the body is not that. */
function errorDetail(text: string): string | undefined {
  try {
    const parsed: unknown = JSON.parse(text);
    const error = isRecord(parsed) ? parsed["error"] : undefined;
    if (isRecord(error) && typeof error["message"] === "string") {
      return typeof error["code"] === "string"
        ? `${error["code"]}: ${error["message"]}`
        : error["message"];
    }
  } catch {
    // Not JSON: the status line is all there is to say.
  }
  return undefined;
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
    throw new ApiFailure("error", detail ?? `The server answered ${status}.`, status);
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

type Call = (method: string, path: string, body?: unknown) => Promise<unknown>;

async function list(call: Call, path: string): Promise<Json[]> {
  const body = await call("GET", path);
  if (!Array.isArray(body)) throw new ApiFailure("error", "The server's answer was not a list.");
  return body.filter(isRecord);
}

interface FlagConfig {
  on: boolean;
  version: number;
}

function toFlag(raw: Json, cfg: FlagConfig | null): FlagRow {
  return {
    key: str(raw["key"]),
    name: str(raw["name"]),
    description: str(raw["description"]),
    tags: strings(raw["tags"]),
    temporary: raw["temporary"] === true,
    lastEvaluated: typeof raw["lastEvaluated"] === "string" ? raw["lastEvaluated"] : null,
    on: cfg?.on ?? null,
    version: cfg?.version ?? null,
  };
}

async function readConfig(call: Call, path: string): Promise<FlagConfig | null> {
  try {
    const body = await call("GET", path);
    if (!isRecord(body) || typeof body["on"] !== "boolean") return null;
    return { on: body["on"], version: typeof body["version"] === "number" ? body["version"] : 0 };
  } catch (error) {
    // A flag with no config in this environment (404) is shown as unknown; any other failure is not.
    if (error instanceof ApiFailure && error.status === 404) return null;
    throw error;
  }
}

/** `fn` over `items`, `limit` at a time, results in order. */
async function mapLimit<T, R>(
  items: T[],
  limit: number,
  fn: (item: T) => Promise<R>,
): Promise<R[]> {
  const results: R[] = [];
  let next = 0;
  async function worker() {
    for (let i = next++; i < items.length; i = next++) {
      const item = items[i];
      if (item !== undefined) results[i] = await fn(item);
    }
  }
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, worker));
  return results;
}

export interface ToggleRequest {
  project: string;
  flag: string;
  env: string;
  on: boolean;
  /** The version the page read; the server answers 409 if the flag has moved on. `null` skips the check. */
  version: number | null;
}

export function createAdminApi(connection: Connection, doFetch: Fetch) {
  const call: Call = async (method, path, body) => {
    const answer = await send(connection, doFetch, { method, path, body });
    check(answer);
    return parseBody(answer);
  };

  return {
    async projects(): Promise<Project[]> {
      return (await list(call, apiPath("projects"))).map((p) => ({
        key: str(p["key"]),
        name: str(p["name"], str(p["key"])),
      }));
    },

    async environments(project: string): Promise<Environment[]> {
      return (await list(call, apiPath("projects", project, "environments"))).map((e) => ({
        key: str(e["key"]),
        name: str(e["name"], str(e["key"])),
      }));
    },

    /** The project's active flags with each one's on/off in `env`. A config that cannot be read leaves `on` null. */
    async flags(project: string, env: string): Promise<FlagRow[]> {
      const raws = await list(call, apiPath("projects", project, "flags"));
      return mapLimit(raws, CONFIG_CONCURRENCY, async (raw) => {
        const path = apiPath("projects", project, "flags", str(raw["key"]), "environments", env);
        return toFlag(raw, await readConfig(call, path));
      });
    },

    /** Turns a flag on or off in one environment. */
    async toggle({ project, flag, env, on, version }: ToggleRequest): Promise<void> {
      const path = apiPath("projects", project, "flags", flag, "environments", env, "toggle");
      await call("POST", path, { on, ...(version === null ? {} : { version }) });
    },

    /** Creates a boolean flag (variations `[true, false]`: off serves false, on serves true). */
    async createFlag(project: string, flag: NewFlag): Promise<void> {
      await call("POST", apiPath("projects", project, "flags"), {
        key: flag.key,
        name: flag.name,
        description: flag.description,
        temporary: flag.temporary,
        tags: flag.tags,
        variations: [true, false],
      });
    },

    async archiveFlag(project: string, flag: string): Promise<void> {
      await call("POST", apiPath("projects", project, "flags", flag, "archive"));
    },
  };
}

export type AdminApi = ReturnType<typeof createAdminApi>;

/**
 * The connection from the Worker's environment (`FEATURESLASH_API_URL`, `FEATURESLASH_ADMIN_TOKEN`), or
 * why there is none. The message names the variable, never a value.
 */
export function readConnection(
  url: unknown,
  token: unknown,
): { ok: true; connection: Connection } | { ok: false; message: string } {
  if (typeof url !== "string" || url.trim() === "") {
    return { ok: false, message: "FEATURESLASH_API_URL is not set." };
  }
  if (typeof token !== "string" || token === "") {
    return { ok: false, message: "FEATURESLASH_ADMIN_TOKEN is not set." };
  }
  let parsed: URL;
  try {
    parsed = new URL(url.trim());
  } catch {
    return { ok: false, message: "FEATURESLASH_API_URL is not a URL." };
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    return { ok: false, message: "FEATURESLASH_API_URL must be an http or https URL." };
  }
  return {
    ok: true,
    connection: { url: parsed.origin + parsed.pathname.replace(/\/+$/, ""), token },
  };
}
