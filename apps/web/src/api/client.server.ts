/**
 * The typed client for the C# server's admin API. It runs only in the Worker (server functions), with
 * the token it is given, and nothing here is imported by a route component. The `.server.ts` name
 * makes TanStack Start's import protection fail the build if a client module ever imports it.
 */
import { str, strings, type Json } from "./json.ts";
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

/** What `GET .../environments/{env}/flag-states` says about one flag in the selected environment. */
interface FlagState {
  on: boolean;
  version: number;
  lastEvaluated: string | null;
}

function toFlag(raw: Json, state: FlagState | undefined): FlagRow {
  return {
    key: str(raw["key"]),
    name: str(raw["name"]),
    description: str(raw["description"]),
    tags: strings(raw["tags"]),
    temporary: raw["temporary"] === true,
    lastEvaluated: state?.lastEvaluated ?? null,
    on: state?.on ?? null,
    version: state?.version ?? null,
  };
}

/** The states by flag key. An environment the server does not know (404) gives none, so every flag is unknown. */
async function readStates(call: Call, path: string): Promise<Map<string, FlagState>> {
  const states = new Map<string, FlagState>();
  try {
    for (const item of await list(call, path)) {
      if (typeof item["on"] !== "boolean") continue;
      states.set(str(item["flag"]), {
        on: item["on"],
        version: typeof item["version"] === "number" ? item["version"] : 0,
        lastEvaluated: typeof item["lastEvaluated"] === "string" ? item["lastEvaluated"] : null,
      });
    }
  } catch (error) {
    // No states for this environment (404) shows every flag as unknown; any other failure is not.
    if (!(error instanceof ApiFailure && error.status === 404)) throw error;
  }
  return states;
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

    /**
     * The project's active flags with each one's on/off, version and last evaluated day in `env`: the flag list
     * and the environment's flag states, one read each, joined on the key. A flag with no state leaves `on` null.
     */
    async flags(project: string, env: string): Promise<FlagRow[]> {
      const [raws, states] = await Promise.all([
        list(call, apiPath("projects", project, "flags")),
        readStates(call, apiPath("projects", project, "environments", env, "flag-states")),
      ]);
      return raws.map((raw) => toFlag(raw, states.get(str(raw["key"]))));
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
 * The connection from the Worker's environment (`TOGGLESLASH_API_URL`, `TOGGLESLASH_ADMIN_TOKEN`), or
 * why there is none. The message names the variable, never a value.
 */
export function readConnection(
  url: unknown,
  token: unknown,
): { ok: true; connection: Connection } | { ok: false; message: string } {
  if (typeof url !== "string" || url.trim() === "") {
    return { ok: false, message: "TOGGLESLASH_API_URL is not set." };
  }
  if (typeof token !== "string" || token === "") {
    return { ok: false, message: "TOGGLESLASH_ADMIN_TOKEN is not set." };
  }
  let parsed: URL;
  try {
    parsed = new URL(url.trim());
  } catch {
    return { ok: false, message: "TOGGLESLASH_API_URL is not a URL." };
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    return { ok: false, message: "TOGGLESLASH_API_URL must be an http or https URL." };
  }
  return {
    ok: true,
    connection: { url: parsed.origin + parsed.pathname.replace(/\/+$/, ""), token },
  };
}
