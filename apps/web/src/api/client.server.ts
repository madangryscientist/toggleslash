/**
 * The typed client for the C# server's admin API. It runs only in the Worker (server functions), with
 * the token it is given, and nothing here is imported by a route component. The `.server.ts` name
 * makes TanStack Start's import protection fail the build if a client module ever imports it.
 */
import { isRecord, str, strings, type Json } from "./json.ts";
import {
  ApiFailure,
  apiPath,
  list,
  makeCall,
  type Connection,
  type Fetch,
  type Call,
} from "./http.server.ts";
import { createDetailApi } from "./detail.server.ts";
import { createOperatorApi, toEnvironment } from "./operator.server.ts";
import type { Environment, FlagRow, NewFlag, Project } from "./types.ts";

export { ApiFailure, apiPath };
export type { Connection, Fetch };
export type { FlagPatch } from "./detail.server.ts";

/** How many per-flag config reads run at once: the list is N+1 until the server has a bulk read. */
const CONFIG_CONCURRENCY = 8;

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
  const call = makeCall(connection, doFetch);

  return {
    ...createDetailApi(call),
    ...createOperatorApi(call),
    async projects(): Promise<Project[]> {
      return (await list(call, apiPath("projects"))).map((p) => ({
        key: str(p["key"]),
        name: str(p["name"], str(p["key"])),
      }));
    },

    async environments(project: string): Promise<Environment[]> {
      return (await list(call, apiPath("projects", project, "environments"))).map(toEnvironment);
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
