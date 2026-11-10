/** The admin API calls of the environments page: create, rename and delete. Worker only. */
import { ApiFailure, apiPath, type Call } from "./http.server.ts";
import { isRecord, str, type Json } from "./json.ts";
import type { Environment } from "./types.ts";

export function toEnvironment(raw: Json): Environment {
  return {
    key: str(raw["key"]),
    name: str(raw["name"], str(raw["key"])),
    createdAt: str(raw["createdAt"]),
  };
}

function asEnvironment(raw: unknown): Environment {
  if (!isRecord(raw)) throw new ApiFailure("error", "The server's answer was not an environment.");
  return toEnvironment(raw);
}

export function createEnvironmentApi(call: Call) {
  return {
    async createEnvironment(project: string, key: string, name: string): Promise<Environment> {
      return asEnvironment(
        await call("POST", apiPath("projects", project, "environments"), { key, name }),
      );
    },

    /** Renames an environment; its key never changes. */
    async renameEnvironment(project: string, env: string, name: string): Promise<Environment> {
      return asEnvironment(
        await call("PATCH", apiPath("projects", project, "environments", env), { name }),
      );
    },

    /** Deletes an environment; the server refuses while any of its SDK keys is not revoked. */
    async deleteEnvironment(project: string, env: string): Promise<void> {
      await call("DELETE", apiPath("projects", project, "environments", env));
    },
  };
}
