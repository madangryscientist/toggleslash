/** The fake API's flag-detail routes: config PUT with the server's validation messages, flag PATCH, history and usage. */
import { db, type AuditEvent, type Config, type Flag } from "./fake-data.ts";

/** A status and a body: what a route answers. */
export type Reply = [number, unknown];
export type Body = Record<string, unknown>;

const WEIGHT_TOTAL = 100000;

export function failure(status: number, code: string, message: string): Reply {
  return [status, { error: { code, message } }];
}

/** A JSON object, or an empty one. */
export function asBody(value: unknown): Body {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? Object.fromEntries(Object.entries(value))
    : {};
}

export interface Recorded {
  project: string;
  flag: string;
  action: string;
  env: string | null;
  version: number | null;
}

export function record({ project, flag, action, env, version }: Recorded) {
  const events = (db.data.history[`${project}/${flag}`] ??= []);
  const id = Math.max(0, ...events.map((e) => e.id)) + 1;
  const event: AuditEvent = {
    id,
    occurredAt: `2026-11-05T08:${String(id % 60).padStart(2, "0")}:00Z`,
    actor: "admin",
    entityType: env === null ? "flag" : "flag_config",
    action,
    environment: env,
    after: version === null ? null : { version },
  };
  events.unshift(event);
}

function records(value: unknown): Body[] {
  return Array.isArray(value) ? value.map(asBody) : [];
}

/** The server's check of one rule or the fallthrough: a variation in range, or a rollout summing to 100000. */
function serveProblem(holder: Body, path: string, count: number): string | null {
  const rollout = asBody(holder["rollout"]);
  if (holder["rollout"] !== undefined) {
    const entries = records(rollout["variations"]);
    const total = entries.reduce((sum, e) => sum + Number(e["weight"] ?? 0), 0);
    return total === WEIGHT_TOTAL
      ? null
      : `${path}.rollout weights must sum to ${WEIGHT_TOTAL}, they sum to ${total}.`;
  }
  const variation = holder["variation"];
  if (typeof variation !== "number" || variation < 0 || variation >= count) {
    return `${path}.variation must be an index into the flag's ${count} variations.`;
  }
  return null;
}

function clauseProblem(clause: Body, path: string): string | null {
  if (
    (clause["op"] !== "segmentMatch" && clause["attribute"] === undefined) ||
    clause["attribute"] === ""
  ) {
    return `${path}.attribute must be a non-empty string.`;
  }
  return null;
}

function ruleProblem(rule: Body, path: string, count: number): string | null {
  for (const [j, clause] of records(rule["clauses"]).entries()) {
    const found = clauseProblem(clause, `${path}.clauses[${j}]`);
    if (found !== null) return found;
  }
  return serveProblem(rule, path, count);
}

/** The first thing wrong with a config body, in the server's words (the path first), or nothing. */
export function configProblem(body: Body, count: number): string | null {
  if (typeof body["on"] !== "boolean") return "on must be a boolean.";
  const off = body["offVariation"];
  if (off !== null && (typeof off !== "number" || off < 0 || off >= count)) {
    return `offVariation must be an index into the flag's ${count} variations.`;
  }
  for (const [i, rule] of records(body["rules"]).entries()) {
    const found = ruleProblem(rule, `rules[${i}]`, count);
    if (found !== null) return found;
  }
  return serveProblem(asBody(body["fallthrough"]), "fallthrough", count);
}

export interface ConfigAt {
  project: string;
  flag: Flag;
  env: string;
}

/** PUT a whole config: validation first, then the version check, then the write. */
export function putConfig(at: ConfigAt, cfg: Config, body: Body): Reply {
  const { project, flag, env } = at;
  const invalid = configProblem(body, flag.variations.length);
  if (invalid !== null) return failure(400, "invalid_config", invalid);
  if (body["version"] !== cfg.version) {
    return failure(
      409,
      "version_conflict",
      `The config is at version ${cfg.version}, not ${String(body["version"])}.`,
    );
  }
  cfg.on = body["on"] === true;
  cfg.offVariation = typeof body["offVariation"] === "number" ? body["offVariation"] : null;
  cfg.targets = records(body["targets"]);
  cfg.prerequisites = records(body["prerequisites"]);
  cfg.rules = records(body["rules"]);
  cfg.fallthrough = asBody(body["fallthrough"]);
  cfg.version += 1;
  record({ project, flag: flag.key, action: "config.updated", env, version: cfg.version });
  return [200, cfg];
}

/** PATCH the metadata; a field left out stays as it is. */
export function patchFlag(project: string, flag: Flag, body: Body): Reply {
  if (typeof body["name"] === "string" && body["name"].trim() === "") {
    return failure(400, "invalid_request", "name must not be empty.");
  }
  if (typeof body["name"] === "string") flag.name = body["name"];
  if (typeof body["description"] === "string") flag.description = body["description"];
  if (typeof body["owner"] === "string") flag.owner = body["owner"] === "" ? null : body["owner"];
  if (typeof body["temporary"] === "boolean") flag.temporary = body["temporary"];
  if (Array.isArray(body["tags"])) flag.tags = body["tags"].map(String);
  record({ project, flag: flag.key, action: "flag.updated", env: null, version: null });
  return [200, flag];
}

export function historyOf(project: string, key: string): Reply {
  return [200, { events: db.data.history[`${project}/${key}`] ?? [] }];
}

/** Fixed usage for checkout-v2 in production; every other flag has none. */
export function usageOf(project: string, key: string): Reply {
  const seen = project === "shop" && key === "checkout-v2";
  return [
    200,
    {
      environments: seen
        ? [
            {
              environment: "production",
              total: 42,
              days: [
                {
                  day: "2026-11-04",
                  count: 30,
                  variations: [
                    { variationIndex: 0, count: 10 },
                    { variationIndex: 1, count: 20 },
                  ],
                },
                { day: "2026-11-03", count: 12, variations: [{ variationIndex: 1, count: 12 }] },
              ],
            },
          ]
        : [],
    },
  ];
}
