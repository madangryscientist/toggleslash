/**
 * A small stand-in for the C# server's admin API, for the Playwright run: `bun e2e/fake-api.ts`.
 * It speaks only the routes the dashboard calls, keeps its data in memory, and has four control routes
 * the specs use: `POST /__reset` (back to the fixture data, server up), `POST /__down` (answer
 * every API request by dropping the connection, as a stopped server would) and
 * `POST /__bump/<project>/<env>/<flag>` and `POST /__bump-segment/<project>/<env>/<segment>` (another
 * editor saved: the config's or the segment's version moves on).
 */
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";

import { API_PORT, E2E_ADMIN_TOKEN } from "./constants.ts";
import { db, flag, plain, reset, type Config, type Flag } from "./fake-data.ts";
import { auditPage, createEnvironment, onKeys, onSegments } from "./fake-operator.ts";
import {
  historyOf,
  patchFlag,
  putConfig,
  record,
  usageOf,
  type Body,
  type Reply,
} from "./fake-config.ts";

function send(res: ServerResponse, status: number, body: unknown) {
  res.writeHead(status, { "content-type": "application/json" });
  res.end(body === undefined ? "" : JSON.stringify(body));
}

function problem(res: ServerResponse, status: number, code: string, message: string) {
  send(res, status, { error: { code, message } });
}

async function readBody(req: IncomingMessage): Promise<Body> {
  const chunks: Buffer[] = [];
  for await (const chunk of req) chunks.push(Buffer.from(chunk));
  const text = Buffer.concat(chunks).toString("utf8");
  if (text === "") return {};
  const parsed: unknown = JSON.parse(text);
  return typeof parsed === "object" && parsed !== null
    ? Object.fromEntries(Object.entries(parsed))
    : {};
}

const KEY = /^[a-z0-9][a-z0-9_-]{0,63}$/;

interface Ctx {
  req: IncomingMessage;
  res: ServerResponse;
  project: string;
}

function activeFlags(project: string): Flag[] {
  return (db.data.flags[project] ??= []);
}

/** Why a create request is refused, or nothing. */
function createProblem(project: string, body: Body): [number, string, string] | null {
  const key = typeof body["key"] === "string" ? body["key"] : "";
  if (!KEY.test(key)) {
    return [400, "invalid_request", "key must match ^[a-z0-9][a-z0-9_-]{0,63}$."];
  }
  if (typeof body["name"] !== "string" || body["name"].trim() === "") {
    return [400, "invalid_request", "name is required."];
  }
  if (activeFlags(project).some((f) => f.key === key)) {
    return [409, "conflict", `A flag with key ${key} already exists.`];
  }
  return null;
}

async function createFlag({ req, res, project }: Ctx) {
  const body = await readBody(req);
  const refused = createProblem(project, body);
  if (refused !== null) return problem(res, ...refused);
  const key = String(body["key"]);
  const created = flag({
    key,
    name: String(body["name"]),
    tags: Array.isArray(body["tags"]) ? body["tags"].map(String) : [],
    temporary: body["temporary"] === true,
    description: typeof body["description"] === "string" ? body["description"] : "",
  });
  activeFlags(project).push(created);
  for (const env of db.data.environments[project] ?? []) {
    db.data.configs[`${project}/${env.key}/${key}`] = plain(false, 1);
  }
  return send(res, 201, created);
}

function reply(res: ServerResponse, [status, body]: Reply) {
  return send(res, status, body);
}

async function toggle({ req, res, project }: Ctx, flagKey: string, env: string, cfg: Config) {
  const body = await readBody(req);
  if (typeof body["on"] !== "boolean") {
    return problem(res, 400, "invalid_request", "on is required.");
  }
  if (typeof body["version"] === "number" && body["version"] !== cfg.version) {
    return problem(res, 409, "version_conflict", "The flag changed since you loaded it.");
  }
  cfg.on = body["on"];
  cfg.version += 1;
  record({ project, flag: flagKey, action: "config.toggled", env, version: cfg.version });
  return send(res, 200, cfg);
}

async function onConfig(ctx: Ctx, found: Flag, rest: string[]) {
  const { req, res, project } = ctx;
  const [env, verb] = rest;
  const method = req.method ?? "GET";
  const cfg = db.data.configs[`${project}/${env}/${found.key}`];
  if (cfg === undefined || env === undefined)
    return problem(res, 404, "not_found", "No such route.");
  if (verb === undefined && method === "GET") return send(res, 200, cfg);
  if (verb === undefined && method === "PUT") {
    return reply(res, putConfig({ project, flag: found, env }, cfg, await readBody(req)));
  }
  if (verb === "toggle" && method === "POST") return toggle(ctx, found.key, env, cfg);
  return problem(res, 404, "not_found", "No such route.");
}

function onFlagRead({ res, project }: Ctx, found: Flag, action: string | undefined) {
  if (action === undefined) return send(res, 200, found);
  if (action === "history") return reply(res, historyOf(project, found.key));
  if (action === "usage") return reply(res, usageOf(project, found.key));
  return problem(res, 404, "not_found", "No such route.");
}

/** `.../flags/<flag>/...`: read, patch, archive, history, usage, and the per-environment configs. */
async function onFlag(ctx: Ctx, rest: string[]) {
  const { req, res, project } = ctx;
  const [key, action, ...more] = rest;
  const found = activeFlags(project).find((f) => f.key === key);
  if (found === undefined) return problem(res, 404, "flag_not_found", `No flag ${key}.`);
  const method = req.method ?? "GET";
  if (action === undefined && method === "PATCH") {
    return reply(res, patchFlag(project, found, await readBody(req)));
  }
  if (action === "archive" && method === "POST") {
    found.archivedAt = "2026-11-05T08:00:00Z";
    return send(res, 200, found);
  }
  if (action === "environments") return onConfig(ctx, found, more);
  return onFlagRead(ctx, found, action);
}

function onFlags(ctx: Ctx, rest: string[]) {
  const { req, res, project } = ctx;
  if (rest.length > 0) return onFlag(ctx, rest);
  if (req.method === "POST") return createFlag(ctx);
  return send(
    res,
    200,
    activeFlags(project).filter((f) => f.archivedAt === null),
  );
}

/** `.../environments/<env>/flag-states`: every active flag's on/off, version and last day in that environment. */
function flagStates(res: ServerResponse, project: string, env: string) {
  if (!(db.data.environments[project] ?? []).some((e) => e.key === env)) {
    return problem(res, 404, "environment_not_found", `No environment ${env}.`);
  }
  const states = activeFlags(project)
    .filter((f) => f.archivedAt === null)
    .flatMap((f) => {
      const cfg = db.data.configs[`${project}/${env}/${f.key}`];
      if (cfg === undefined) return [];
      const last = db.data.lastEvaluated[`${project}/${env}/${f.key}`] ?? null;
      return [{ flag: f.key, on: cfg.on, version: cfg.version, lastEvaluated: last }];
    });
  return send(res, 200, states);
}

/** `.../environments/<env>/keys|segments/...`: the operator routes under one environment. */
async function onEnvironment({ req, res, project }: Ctx, [env = "", what, ...more]: string[]) {
  if (what === "flag-states") return flagStates(res, project, env);
  const method = req.method ?? "GET";
  const body = method === "PUT" || method === "POST" ? await readBody(req) : {};
  const call = { project, env, method, rest: more, body };
  if (what === "keys") return reply(res, onKeys(call));
  if (what === "segments") return reply(res, onSegments(call));
  return problem(res, 404, "not_found", "No such route.");
}

async function onProject(ctx: Ctx, rest: string[], query: URLSearchParams) {
  const { req, res, project } = ctx;
  const [kind, ...more] = rest;
  if (kind === "environments" && more.length === 0) {
    if (req.method === "POST") {
      return reply(res, createEnvironment(project, await readBody(req)));
    }
    return send(res, 200, db.data.environments[project] ?? []);
  }
  if (kind === "environments") return onEnvironment(ctx, more);
  if (kind === "audit" && more.length === 0) return reply(res, auditPage(project, query));
  if (kind === "flags") return onFlags(ctx, more);
  return problem(res, 404, "not_found", "No such route.");
}

/** `segments` are what follows `/api/v1`. */
async function api(
  req: IncomingMessage,
  res: ServerResponse,
  segments: string[],
  query: URLSearchParams,
) {
  const [projects, project, ...rest] = segments;
  if (projects !== "projects") return problem(res, 404, "not_found", "No such route.");
  if (project === undefined) return send(res, 200, db.data.projects);
  if (!db.data.projects.some((p) => p.key === project)) {
    return problem(res, 404, "project_not_found", `No project ${project}.`);
  }
  return onProject({ req, res, project }, rest, query);
}

/** `POST /__bump/<project>/<env>/<flag>`: someone else saved that config, so its version moves on. */
function bump(res: ServerResponse, path: string) {
  const [, , project = "", env = "", key = ""] = path.split("/");
  const cfg = db.data.configs[`${project}/${env}/${key}`];
  if (cfg === undefined) return problem(res, 404, "not_found", "No such config.");
  cfg.version += 1;
  record({ project, flag: key, action: "config.updated", env, version: cfg.version });
  return send(res, 200, cfg);
}

/** `POST /__bump-segment/<project>/<env>/<segment>`: someone else saved that segment. */
function bumpSegment(res: ServerResponse, path: string) {
  const [, , project = "", env = "", key = ""] = path.split("/");
  const found = db.data.segments[`${project}/${env}`]?.find((s) => s.key === key);
  if (found === undefined) return problem(res, 404, "not_found", "No such segment.");
  found.included = ["someone-else"];
  found.version += 1;
  return send(res, 200, found);
}

createServer((req, res) => {
  const url = new URL(req.url ?? "/", "http://fake");
  if (url.pathname === "/__reset") {
    reset();
    return send(res, 200, { ok: true });
  }
  if (url.pathname.startsWith("/__bump/")) return bump(res, url.pathname);
  if (url.pathname.startsWith("/__bump-segment/")) return bumpSegment(res, url.pathname);
  if (url.pathname === "/__down") {
    db.down = true;
    return send(res, 200, { ok: true });
  }
  if (url.pathname === "/health") return send(res, 200, { status: "ok" });
  if (db.down) {
    req.socket.destroy();
    return;
  }
  if (req.headers.authorization !== `Bearer ${E2E_ADMIN_TOKEN}`) {
    return problem(res, 401, "unauthorized", "Missing or wrong admin token.");
  }
  api(req, res, url.pathname.split("/").slice(3).map(decodeURIComponent), url.searchParams).catch(
    () => {
      problem(res, 500, "internal", "The fake API failed.");
    },
  );
}).listen(API_PORT, "127.0.0.1", () => {
  process.stdout.write(`fake API on http://127.0.0.1:${API_PORT}\n`);
});
