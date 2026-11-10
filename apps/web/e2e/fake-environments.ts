/** The fake API's environment routes: create, rename and delete, with the server's statuses and messages. */
import { failure, type Body, type Reply } from "./fake-config.ts";
import { db } from "./fake-data.ts";

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

const notFound = () => failure(404, "not_found", "No such environment.");

/** `PATCH .../environments/<env>`: the name changes, the key never does. */
export function renameEnvironment(project: string, env: string, body: Body): Reply {
  const found = (db.data.environments[project] ?? []).find((e) => e.key === env);
  if (found === undefined) return notFound();
  if (typeof body["name"] !== "string" || body["name"].trim() === "") {
    return failure(400, "invalid_request", "name is required.");
  }
  found.name = body["name"].trim();
  return [200, found];
}

/** `DELETE .../environments/<env>`: refused while a key is not revoked; otherwise its keys, segments and configs go too. */
export function deleteEnvironment(project: string, env: string): Reply {
  const list = db.data.environments[project] ?? [];
  const index = list.findIndex((e) => e.key === env);
  if (index < 0) return notFound();
  const id = `${project}/${env}`;
  const live = (db.data.keys[id] ?? []).filter((k) => k.revokedAt === null).length;
  if (live > 0) {
    return failure(
      409,
      "conflict",
      `Environment "${env}" still has ${live} SDK key(s) that are not revoked. Revoke them first.`,
    );
  }
  list.splice(index, 1);
  delete db.data.keys[id];
  delete db.data.segments[id];
  for (const config of Object.keys(db.data.configs)) {
    if (config.startsWith(`${id}/`)) delete db.data.configs[config];
  }
  return [204, undefined];
}
