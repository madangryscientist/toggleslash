/** The admin API calls the flag detail page makes: one flag, its config, history and usage. Worker only. */
import { ApiFailure, apiPath, list, type Call } from "./http.server.ts";
import { isRecord, lifecycleOf, str, strings, type Json } from "./json.ts";
import { parseConfig, type Config } from "../targeting/model.ts";
import type { Wire } from "../targeting/wire.ts";
import type { FlagDetail, HistoryEvent, Sibling, Usage } from "./types.ts";

const HISTORY_LIMIT = 30;

function configPath(project: string, flag: string, env: string): string {
  return apiPath("projects", project, "flags", flag, "environments", env);
}

/** A variation value as a person reads it: `true`, `"blue"`, `42`. */
function variationLabels(value: unknown): string[] {
  return Array.isArray(value) ? value.map((v) => JSON.stringify(v) ?? "null") : [];
}

function toDetail(raw: unknown): FlagDetail {
  if (!isRecord(raw)) throw new ApiFailure("error", "The server's answer was not a flag.");
  return {
    key: str(raw["key"]),
    name: str(raw["name"]),
    description: str(raw["description"]),
    owner: typeof raw["owner"] === "string" ? raw["owner"] : "",
    tags: strings(raw["tags"]),
    temporary: raw["temporary"] === true,
    archived: typeof raw["archivedAt"] === "string",
    lifecycle: lifecycleOf(raw["lifecycle"]),
    variations: variationLabels(raw["variations"]),
    lastEvaluated: typeof raw["lastEvaluated"] === "string" ? raw["lastEvaluated"] : null,
  };
}

function toEvent(raw: Json): HistoryEvent {
  const after = isRecord(raw["after"]) ? raw["after"] : null;
  return {
    id: typeof raw["id"] === "number" ? raw["id"] : 0,
    at: str(raw["occurredAt"]),
    actor: str(raw["actor"]),
    entityType: str(raw["entityType"]),
    action: str(raw["action"]),
    environment: typeof raw["environment"] === "string" ? raw["environment"] : null,
    version: after !== null && typeof after["version"] === "number" ? after["version"] : null,
  };
}

function toUsage(raw: unknown): Usage {
  const environments =
    isRecord(raw) && Array.isArray(raw["environments"]) ? raw["environments"] : [];
  return {
    environments: environments.filter(isRecord).map((e) => ({
      environment: str(e["environment"]),
      total: typeof e["total"] === "number" ? e["total"] : 0,
      days: (Array.isArray(e["days"]) ? e["days"] : []).filter(isRecord).map((d) => ({
        day: str(d["day"]),
        count: typeof d["count"] === "number" ? d["count"] : 0,
        variations: (Array.isArray(d["variations"]) ? d["variations"] : [])
          .filter(isRecord)
          .map((v) => ({
            variation: typeof v["variationIndex"] === "number" ? v["variationIndex"] : 0,
            count: typeof v["count"] === "number" ? v["count"] : 0,
          })),
      })),
    })),
  };
}

export interface FlagPatch {
  name: string;
  description: string;
  owner: string;
  temporary: boolean;
  tags: string[];
}

/** Which flag config in which environment. */
export interface ConfigAt {
  project: string;
  flag: string;
  env: string;
}

export function createDetailApi(call: Call) {
  return {
    async flag(project: string, flag: string): Promise<FlagDetail> {
      return toDetail(await call("GET", apiPath("projects", project, "flags", flag)));
    },

    /** Every active flag in the project with its variations: what a prerequisite can point at. */
    async siblings(project: string): Promise<Sibling[]> {
      return (await list(call, apiPath("projects", project, "flags"))).map((raw) => ({
        key: str(raw["key"]),
        name: str(raw["name"]),
        variations: variationLabels(raw["variations"]),
      }));
    },

    /** Metadata only; a field left out stays as it is. */
    async updateFlag(project: string, flag: string, patch: FlagPatch): Promise<FlagDetail> {
      return toDetail(await call("PATCH", apiPath("projects", project, "flags", flag), patch));
    },

    /** The flag's config in `env`, or `null` when it has none there. */
    async config(project: string, flag: string, env: string): Promise<Config | null> {
      try {
        return parseConfig(await call("GET", configPath(project, flag, env)));
      } catch (error) {
        if (error instanceof ApiFailure && error.status === 404) return null;
        throw error;
      }
    },

    /** Replaces the whole config; the server answers 409 if `version` is no longer current. */
    async putConfig(at: ConfigAt, wire: Wire, version: number): Promise<Config> {
      const saved = parseConfig(
        await call("PUT", configPath(at.project, at.flag, at.env), { ...wire, version }),
      );
      if (saved === null) throw new ApiFailure("error", "The server's answer was not a config.");
      return saved;
    },

    /** The flag's newest audit events, in every environment. */
    async history(project: string, flag: string): Promise<HistoryEvent[]> {
      const body = await call(
        "GET",
        `${apiPath("projects", project, "flags", flag, "history")}?limit=${HISTORY_LIMIT}`,
      );
      const events = isRecord(body) && Array.isArray(body["events"]) ? body["events"] : [];
      return events.filter(isRecord).map(toEvent);
    },

    async usage(project: string, flag: string): Promise<Usage> {
      return toUsage(await call("GET", apiPath("projects", project, "flags", flag, "usage")));
    },
  };
}
