/**
 * The fake API's environment routes: create, rename, delete, secure mode and the signing key, with the
 * server's statuses and messages.
 */
import { generateKeyPairSync } from "node:crypto";
import { failure, type Body, type Reply } from "./fake-config.ts";
import { db, environmentRow } from "./fake-data.ts";

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
  const made = { ...environmentRow(key, body["name"]), createdAt: NOW };
  list.push(made);
  return [201, made];
}

const notFound = () => failure(404, "not_found", "No such environment.");

/** What is wrong with a `PATCH` body, or `null`: both fields are optional, and `null` is as good as absent. */
function updateProblem(body: Body): string | null {
  const name = body["name"] ?? undefined;
  const secureMode = body["secureMode"] ?? undefined;
  if (name !== undefined && (typeof name !== "string" || name.trim() === "")) {
    return "name is required.";
  }
  if (secureMode !== undefined && typeof secureMode !== "boolean") {
    return "secureMode must be true or false.";
  }
  return null;
}

/**
 * `PATCH .../environments/<env>`: the name (the key never changes) and secure mode, each optional. Secure
 * mode on with no signing key is a 409, as the server answers it.
 */
export function updateEnvironment(project: string, env: string, body: Body): Reply {
  const found = (db.data.environments[project] ?? []).find((e) => e.key === env);
  if (found === undefined) return notFound();
  const invalid = updateProblem(body);
  if (invalid !== null) return failure(400, "invalid_request", invalid);
  const name = body["name"];
  const secureMode = body["secureMode"];
  if (secureMode === true && found.signingKeyCreatedAt === null) {
    return failure(
      409,
      "conflict",
      `Environment "${env}" has no signing key, so nothing could sign for it. Make one first, then turn secure mode on.`,
    );
  }
  if (typeof name === "string") found.name = name.trim();
  if (typeof secureMode === "boolean") found.secureMode = secureMode;
  return [200, found];
}

/**
 * `POST .../environments/<env>/signing-key`: a new P-256 pair, made fresh for every call and kept nowhere;
 * the fake answers its private half once and keeps only the date, as the server keeps only the public half.
 */
export function createSigningKey(project: string, env: string): Reply {
  const found = (db.data.environments[project] ?? []).find((e) => e.key === env);
  if (found === undefined) return notFound();
  const { privateKey } = generateKeyPairSync("ec", { namedCurve: "P-256" });
  const createdAt = new Date().toISOString();
  found.signingKeyCreatedAt = createdAt;
  return [201, { privateKey: privateKey.export({ type: "pkcs8", format: "pem" }), createdAt }];
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
