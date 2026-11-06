/** The fake API's operator routes: environments, SDK keys, segments and the project audit log. */
import { PAST_RULESET_HASH, PLANTED_SALT } from "./constants.ts";
import { asBody, failure, type Body, type Reply } from "./fake-config.ts";
import { db, type KeyRow, type SegmentRow } from "./fake-data.ts";

const KEY = /^[a-z0-9][a-z0-9_-]{0,63}$/;
const NOW = "2026-11-05T08:00:00Z";

export function createEnvironment(project: string, body: Body): Reply {
  const key = typeof body["key"] === "string" ? body["key"] : "";
  if (!KEY.test(key)) {
    return failure(400, "invalid_request", "key must match ^[a-z0-9][a-z0-9_-]{0,63}$.");
  }
  if (typeof body["name"] !== "string" || body["name"].trim() === "") {
    return failure(400, "invalid_request", "name is required.");
  }
  const list = (db.data.environments[project] ??= []);
  if (list.some((e) => e.key === key)) {
    return failure(409, "conflict", `An environment with key ${key} already exists.`);
  }
  const made = { key, name: body["name"], createdAt: NOW };
  list.push(made);
  return [201, made];
}

function keysOf(project: string, env: string): KeyRow[] | null {
  if (!(db.data.environments[project] ?? []).some((e) => e.key === env)) return null;
  return (db.data.keys[`${project}/${env}`] ??= []);
}

let counter = 0;

/** One request under an environment: its project, environment, verb, the path after the collection and the body. */
interface Call {
  project: string;
  env: string;
  method: string;
  rest: string[];
  body: Body;
}

function createKey({ body }: Call, keys: KeyRow[]): Reply {
  const kind = body["kind"];
  if (kind !== "server" && kind !== "client") {
    return failure(400, "invalid_request", "kind must be server or client.");
  }
  if (typeof body["name"] !== "string" || body["name"].trim() === "") {
    return failure(400, "invalid_request", "name is required.");
  }
  counter += 1;
  const secret = `fs_${kind === "server" ? "srv" : "cli"}_${String(counter).padStart(8, "0")}SECRETPART`;
  const row: KeyRow = {
    id: Math.max(0, ...keys.map((k) => k.id)) + 1,
    kind,
    name: body["name"],
    prefix: secret.slice(0, 15),
    createdAt: NOW,
    revokedAt: null,
  };
  keys.push(row);
  return [201, { ...row, key: secret }];
}

function revokeKey(found: KeyRow): Reply {
  found.revokedAt ??= NOW;
  return [204, undefined];
}

/** `.../environments/<env>/keys[/<id>]`: list, create (the only answer with the plaintext) and revoke. */
export function onKeys(call: Call): Reply {
  const { project, env, method, rest } = call;
  const keys = keysOf(project, env);
  if (keys === null) return failure(404, "environment_not_found", `No environment ${env}.`);
  const [id] = rest;
  if (id === undefined) {
    if (method === "GET") return [200, keys];
    if (method === "POST") return createKey(call, keys);
    return failure(404, "not_found", "No such route.");
  }
  const found = keys.find((k) => String(k.id) === id);
  if (found === undefined) return failure(404, "key_not_found", `No key ${id}.`);
  if (method === "DELETE") return revokeKey(found);
  return failure(404, "not_found", "No such route.");
}

function segmentsOf(project: string, env: string): SegmentRow[] | null {
  if (!(db.data.environments[project] ?? []).some((e) => e.key === env)) return null;
  return (db.data.segments[`${project}/${env}`] ??= []);
}

function names(value: unknown): string[] | null {
  return Array.isArray(value) && value.every((v) => typeof v === "string") ? value : null;
}

/** Validation, then the version check, then the write. */
function putSegment(found: SegmentRow, body: Body): Reply {
  const included = names(body["included"]);
  const excluded = names(body["excluded"]);
  if (included === null || excluded === null) {
    return failure(400, "invalid_request", "included and excluded must be lists of context keys.");
  }
  if (!Array.isArray(body["rules"])) {
    return failure(400, "invalid_request", "rules must be a list.");
  }
  const bad = body["rules"].findIndex((r) => !Array.isArray(asBody(r)["clauses"]));
  if (bad >= 0) return failure(400, "invalid_request", `rules[${bad}].clauses must be a list.`);
  if (body["version"] !== found.version) {
    return failure(
      409,
      "version_conflict",
      `The segment is at version ${found.version}, not ${String(body["version"])}.`,
    );
  }
  found.included = included;
  found.excluded = excluded;
  found.rules = body["rules"];
  found.version += 1;
  return [200, found];
}

/** `.../environments/<env>/segments[/<segment>]`: list, and replace one (the server's own message for a bad body). */
export function onSegments({ project, env, method, rest, body }: Call): Reply {
  const segments = segmentsOf(project, env);
  if (segments === null) return failure(404, "environment_not_found", `No environment ${env}.`);
  const [key] = rest;
  if (key === undefined && method === "GET") return [200, segments];
  const found = segments.find((s) => s.key === key);
  if (found === undefined) return failure(404, "segment_not_found", `No segment ${key}.`);
  if (method !== "PUT") return failure(404, "not_found", "No such route.");
  return putSegment(found, body);
}

/** `GET .../audit?flag=&environment=&since=&limit=&cursor=`: newest first, a cursor being the last id returned. */
export function auditPage(project: string, query: URLSearchParams): Reply {
  const flag = query.get("flag");
  const environment = query.get("environment");
  const since = query.get("since");
  const cursor = query.get("cursor");
  const limit = Math.min(100, Math.max(1, Number(query.get("limit") ?? 50) || 50));
  const matching = (db.data.audit[project] ?? []).filter(
    (e) =>
      (flag === null || e.flag === flag) &&
      (environment === null || e.environment === environment) &&
      (since === null || e.occurredAt >= since) &&
      (cursor === null || e.id < Number(cursor)),
  );
  const events = matching.slice(0, limit);
  const last = events.at(-1);
  const nextCursor = matching.length > limit && last !== undefined ? String(last.id) : null;
  return [200, { events, nextCursor }];
}

/**
 * `GET .../environments/<env>/rulesets/<hash>[?flag=]`: what shop/production served under one hash. The flags are
 * as they were then, not as the fake's current configs are: checkout-v2 was off with no rules and dark-mode on.
 * The real route has no salt. The fake sends one anyway (PLANTED_SALT, on each flag and segment) so the spec can show
 * the dashboard would not pass it on. A hash it never served (or another environment's) is a 404.
 */
export function rulesetAt(
  project: string,
  env: string,
  hash: string,
  query: URLSearchParams,
): Reply {
  if (project !== "shop" || env !== "production" || hash !== PAST_RULESET_HASH) {
    return failure(
      404,
      "not_found",
      "This environment never served a ruleset with that hash. A hash is the ETag's lowercase hex, without quotes.",
    );
  }
  const old = {
    etag: `"${PAST_RULESET_HASH}"`,
    firstServedAt: "2026-11-02T09:30:00Z",
    flags: [
      {
        key: "checkout-v2",
        variations: [true, false],
        on: false,
        offVariation: 1,
        targets: [],
        prerequisites: [],
        rules: [
          {
            clauses: [{ op: "in", attribute: "country", values: ["NL"] }],
            variation: 0,
          },
        ],
        fallthrough: { variation: 1 },
        version: 2,
        salt: PLANTED_SALT,
      },
      {
        key: "dark-mode",
        variations: [true, false],
        on: true,
        offVariation: 1,
        targets: [{ variation: 0, contextKeys: ["alice"] }],
        prerequisites: [],
        rules: [],
        fallthrough: { variation: 1 },
        version: 1,
        salt: PLANTED_SALT,
      },
    ],
    segments: [
      {
        key: "beta-testers",
        included: ["alice"],
        excluded: [],
        rules: [{ clauses: [{ op: "in", attribute: "country", values: ["NL"] }] }],
        salt: PLANTED_SALT,
      },
    ],
  };
  const only = query.get("flag");
  if (only === null) return [200, old];
  const flags = old.flags.filter((f) => f.key === only);
  if (flags.length === 0) return failure(404, "not_found", "That ruleset has no such flag.");
  return [200, { ...old, flags, segments: [] }];
}
