/**
 * The admin API calls the operator pages make: environments, SDK keys, segments and the audit log.
 * Worker only. A key's plaintext appears in exactly one answer (`createKey`) and is never cached here.
 */
import { ApiFailure, apiPath, list, type Call } from "./http.server.ts";
import { isRecord, str, strings, type Json } from "./json.ts";
import { parseConfig } from "../targeting/model.ts";
import type {
  ApiKey,
  AuditEvent,
  AuditFilters,
  AuditPage,
  CreatedKey,
  Environment,
  KeyKind,
  PastFlag,
  PastRuleset,
  PastSegment,
  Segment,
} from "./types.ts";

export const AUDIT_PAGE_SIZE = 25;

export function toEnvironment(raw: Json): Environment {
  return {
    key: str(raw["key"]),
    name: str(raw["name"], str(raw["key"])),
    createdAt: str(raw["createdAt"]),
  };
}

function toKey(raw: Json): ApiKey {
  return {
    id: typeof raw["id"] === "number" ? raw["id"] : 0,
    kind: raw["kind"] === "client" ? "client" : "server",
    name: str(raw["name"]),
    prefix: str(raw["prefix"]),
    createdAt: str(raw["createdAt"]),
    revokedAt: typeof raw["revokedAt"] === "string" ? raw["revokedAt"] : null,
  };
}

function toSegment(raw: unknown): Segment {
  if (!isRecord(raw)) throw new ApiFailure("error", "The server's answer was not a segment.");
  return {
    key: str(raw["key"]),
    included: strings(raw["included"]),
    excluded: strings(raw["excluded"]),
    rules: JSON.stringify(Array.isArray(raw["rules"]) ? raw["rules"] : [], null, 2),
    version: typeof raw["version"] === "number" ? raw["version"] : 0,
  };
}

function toPastFlag(raw: Json): PastFlag {
  const config = parseConfig(raw);
  if (config === null)
    throw new ApiFailure("error", "The server's answer held a flag this page cannot read.");
  const variations = Array.isArray(raw["variations"]) ? raw["variations"] : [];
  return {
    key: str(raw["key"]),
    variations: variations.map((v) => JSON.stringify(v) ?? "null"),
    config,
  };
}

function toPastSegment(raw: Json): PastSegment {
  return {
    key: str(raw["key"]),
    included: strings(raw["included"]),
    excluded: strings(raw["excluded"]),
    rules: JSON.stringify(Array.isArray(raw["rules"]) ? raw["rules"] : [], null, 2),
  };
}

/** Only the fields the page shows are copied, so nothing the server adds later reaches the browser unasked. */
function toPastRuleset(hash: string, raw: unknown): PastRuleset {
  if (!isRecord(raw)) throw new ApiFailure("error", "The server's answer was not a ruleset.");
  const flags = Array.isArray(raw["flags"]) ? raw["flags"].filter(isRecord) : [];
  const segments = Array.isArray(raw["segments"]) ? raw["segments"].filter(isRecord) : [];
  return {
    hash,
    firstServedAt: str(raw["firstServedAt"]),
    flags: flags.map(toPastFlag),
    segments: segments.map(toPastSegment),
  };
}

function toEvent(raw: Json): AuditEvent {
  return {
    id: typeof raw["id"] === "number" ? raw["id"] : 0,
    at: str(raw["occurredAt"]),
    actor: str(raw["actor"]),
    entityType: str(raw["entityType"]),
    entityId: str(raw["entityId"]),
    action: str(raw["action"]),
    environment: typeof raw["environment"] === "string" ? raw["environment"] : null,
    flag: typeof raw["flag"] === "string" ? raw["flag"] : null,
  };
}

function auditQuery(filters: AuditFilters, cursor: string | null): string {
  const query = new URLSearchParams({ limit: String(AUDIT_PAGE_SIZE) });
  if (filters.flag !== "") query.set("flag", filters.flag);
  if (filters.environment !== "") query.set("environment", filters.environment);
  // The date input gives `yyyy-mm-dd`; the server wants a time, and the day starts at midnight UTC.
  if (filters.since !== "") query.set("since", `${filters.since}T00:00:00Z`);
  if (cursor !== null) query.set("cursor", cursor);
  return query.toString();
}

export function createOperatorApi(call: Call) {
  return {
    async createEnvironment(project: string, key: string, name: string): Promise<Environment> {
      const made = await call("POST", apiPath("projects", project, "environments"), { key, name });
      if (!isRecord(made))
        throw new ApiFailure("error", "The server's answer was not an environment.");
      return toEnvironment(made);
    },

    /** Every key of the environment, revoked ones included. Never a hash or a plaintext. */
    async keys(project: string, env: string): Promise<ApiKey[]> {
      return (await list(call, apiPath("projects", project, "environments", env, "keys"))).map(
        toKey,
      );
    },

    /** The one answer that carries the plaintext. The caller shows it once and drops it. */
    async createKey(
      project: string,
      env: string,
      kind: KeyKind,
      name: string,
    ): Promise<CreatedKey> {
      const made = await call("POST", apiPath("projects", project, "environments", env, "keys"), {
        kind,
        name,
      });
      if (!isRecord(made) || typeof made["key"] !== "string") {
        throw new ApiFailure("error", "The server's answer was not a new key.");
      }
      return { ...toKey(made), plaintext: made["key"] };
    },

    async revokeKey(project: string, env: string, id: number): Promise<void> {
      await call("DELETE", apiPath("projects", project, "environments", env, "keys", String(id)));
    },

    async segments(project: string, env: string): Promise<Segment[]> {
      const path = apiPath("projects", project, "environments", env, "segments");
      return (await list(call, path)).map(toSegment);
    },

    /** Replaces the segment's members and rules, if it is still at `version`. `rules` is the parsed JSON the page edited. */
    async putSegment(
      project: string,
      env: string,
      segment: string,
      body: { included: string[]; excluded: string[]; rules: unknown; version: number },
    ): Promise<Segment> {
      const path = apiPath("projects", project, "environments", env, "segments", segment);
      return toSegment(await call("PUT", path, body));
    },

    /**
     * What the flags were under a ruleset hash the environment served, without salts. A hash it never served is a
     * value (`found.ok` false) with the server's own words, not a throw: that is a page, not a failure.
     */
    async ruleset(
      project: string,
      env: string,
      hash: string,
      flag: string | null,
    ): Promise<{ ok: true; ruleset: PastRuleset } | { ok: false; message: string }> {
      const base = apiPath("projects", project, "environments", env, "rulesets", hash);
      const path = flag === null ? base : `${base}?flag=${encodeURIComponent(flag)}`;
      try {
        return { ok: true, ruleset: toPastRuleset(hash, await call("GET", path)) };
      } catch (error) {
        if (error instanceof ApiFailure && error.status === 404) {
          return { ok: false, message: error.detail ?? error.message };
        }
        throw error;
      }
    },

    /** One page of the project's audit events, newest first. */
    async audit(project: string, filters: AuditFilters, cursor: string | null): Promise<AuditPage> {
      const path = `${apiPath("projects", project, "audit")}?${auditQuery(filters, cursor)}`;
      const body = await call("GET", path);
      const events = isRecord(body) && Array.isArray(body["events"]) ? body["events"] : [];
      return {
        events: events.filter(isRecord).map(toEvent),
        nextCursor:
          isRecord(body) && typeof body["nextCursor"] === "string" ? body["nextCursor"] : null,
      };
    },
  };
}
