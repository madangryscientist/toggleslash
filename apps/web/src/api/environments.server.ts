/**
 * The admin API calls of the environments page: create, rename, delete, secure mode and the signing key.
 * Worker only. A signing key's private half is in exactly one answer (`createSigningKey`) and is never kept here.
 */
import { ApiFailure, apiPath, type Call } from "./http.server.ts";
import { isRecord, str, type Json } from "./json.ts";
import type { Environment, SigningKey } from "./types.ts";

export function toEnvironment(raw: Json): Environment {
  return {
    key: str(raw["key"]),
    name: str(raw["name"], str(raw["key"])),
    createdAt: str(raw["createdAt"]),
    secureMode: raw["secureMode"] === true,
    signingKeyCreatedAt:
      typeof raw["signingKeyCreatedAt"] === "string" ? raw["signingKeyCreatedAt"] : null,
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

    /** Turns secure mode on or off; the server refuses on (409) while the environment has no signing key. */
    async setSecureMode(project: string, env: string, secureMode: boolean): Promise<Environment> {
      return asEnvironment(
        await call("PATCH", apiPath("projects", project, "environments", env), { secureMode }),
      );
    },

    /** The one answer that carries a signing key's private half; it replaces any old key at once. */
    async createSigningKey(project: string, env: string): Promise<SigningKey> {
      const made = await call(
        "POST",
        apiPath("projects", project, "environments", env, "signing-key"),
      );
      if (!isRecord(made) || typeof made["privateKey"] !== "string") {
        throw new ApiFailure("error", "The server's answer was not a signing key.");
      }
      return { privateKey: made["privateKey"], createdAt: str(made["createdAt"]) };
    },

    /** Deletes an environment; the server refuses while any of its SDK keys is not revoked. */
    async deleteEnvironment(project: string, env: string): Promise<void> {
      await call("DELETE", apiPath("projects", project, "environments", env));
    },
  };
}
