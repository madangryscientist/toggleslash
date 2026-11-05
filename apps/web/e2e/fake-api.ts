/**
 * A small stand-in for the C# server's admin API, for the Playwright run: `bun e2e/fake-api.ts`.
 * It speaks only the routes the dashboard calls, keeps its data in memory, and has two control routes
 * the specs use: `POST /__reset` (back to the fixture data, server up) and `POST /__down` (answer
 * every API request by dropping the connection, as a stopped server would).
 */
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";

import { API_PORT, E2E_ADMIN_TOKEN } from "./constants.ts";

interface Flag {
  key: string;
  name: string;
  description: string;
  variations: boolean[];
  temporary: boolean;
  owner: null;
  tags: string[];
  createdAt: string;
  archivedAt: string | null;
  lastEvaluated: string | null;
}

interface Config {
  on: boolean;
  version: number;
}

interface Named {
  key: string;
  name: string;
  createdAt: string;
}

interface Store {
  projects: Named[];
  environments: Record<string, Named[]>;
  flags: Record<string, Flag[]>;
  /** `project/env/flag` -> config */
  configs: Record<string, Config>;
}

interface Seed {
  key: string;
  name: string;
  tags?: string[];
  temporary?: boolean;
  description?: string;
  last?: string | null;
}

function flag(seed: Seed): Flag {
  return {
    key: seed.key,
    name: seed.name,
    description: seed.description ?? "",
    variations: [true, false],
    temporary: seed.temporary ?? false,
    owner: null,
    tags: seed.tags ?? [],
    createdAt: "2026-11-01T09:00:00Z",
    archivedAt: null,
    lastEvaluated: seed.last ?? null,
  };
}

function named(key: string, name: string): Named {
  return { key, name, createdAt: "2026-11-01T09:00:00Z" };
}

function fixture(): Store {
  return {
    projects: [named("shop", "Shop"), named("docs", "Docs")],
    environments: {
      shop: [named("production", "Production"), named("staging", "Staging")],
      docs: [named("live", "Live")],
    },
    flags: {
      shop: [
        flag({
          key: "checkout-v2",
          name: "Checkout v2",
          tags: ["checkout"],
          temporary: true,
          last: "2026-11-04",
        }),
        flag({ key: "dark-mode", name: "Dark mode", tags: ["ui"] }),
        flag({
          key: "search-boost",
          name: "Search boost",
          tags: ["search", "ui"],
          last: "2026-10-30",
        }),
      ],
      docs: [flag({ key: "docs-search", name: "Docs search" })],
    },
    configs: {
      "shop/production/checkout-v2": { on: true, version: 3 },
      "shop/production/dark-mode": { on: false, version: 1 },
      "shop/production/search-boost": { on: false, version: 2 },
      "shop/staging/checkout-v2": { on: true, version: 1 },
      "shop/staging/dark-mode": { on: true, version: 1 },
      "shop/staging/search-boost": { on: true, version: 1 },
      "docs/live/docs-search": { on: false, version: 1 },
    },
  };
}

let data = fixture();
let down = false;

function send(res: ServerResponse, status: number, body: unknown) {
  res.writeHead(status, { "content-type": "application/json" });
  res.end(body === undefined ? "" : JSON.stringify(body));
}

function problem(res: ServerResponse, status: number, code: string, message: string) {
  send(res, status, { error: { code, message } });
}

type Body = Record<string, unknown>;

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
  return (data.flags[project] ??= []);
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
  for (const env of data.environments[project] ?? []) {
    data.configs[`${project}/${env.key}/${key}`] = { on: false, version: 1 };
  }
  return send(res, 201, created);
}

async function toggle({ req, res }: Ctx, cfg: Config) {
  const body = await readBody(req);
  if (typeof body["on"] !== "boolean") {
    return problem(res, 400, "invalid_request", "on is required.");
  }
  if (typeof body["version"] === "number" && body["version"] !== cfg.version) {
    return problem(res, 409, "version_conflict", "The flag changed since you loaded it.");
  }
  cfg.on = body["on"];
  cfg.version += 1;
  return send(res, 200, cfg);
}

function onConfig(ctx: Ctx, key: string, rest: string[]) {
  const { req, res, project } = ctx;
  const [env, verb] = rest;
  const method = req.method ?? "GET";
  const cfg = data.configs[`${project}/${env}/${key}`];
  if (cfg === undefined) return problem(res, 404, "not_found", "No such route.");
  if (verb === undefined && method === "GET") return send(res, 200, cfg);
  if (verb === "toggle" && method === "POST") return toggle(ctx, cfg);
  return problem(res, 404, "not_found", "No such route.");
}

/** `.../flags/<flag>/...`: archive, read a config, toggle. */
async function onFlag(ctx: Ctx, rest: string[]) {
  const { req, res, project } = ctx;
  const [key, action, ...more] = rest;
  const found = activeFlags(project).find((f) => f.key === key);
  if (found === undefined) return problem(res, 404, "flag_not_found", `No flag ${key}.`);
  if (action === "archive" && req.method === "POST") {
    found.archivedAt = "2026-11-05T08:00:00Z";
    return send(res, 200, found);
  }
  if (action === "environments") return onConfig(ctx, found.key, more);
  return problem(res, 404, "not_found", "No such route.");
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

function onProject(ctx: Ctx, rest: string[]) {
  const { res, project } = ctx;
  const [kind, ...more] = rest;
  if (kind === "environments" && more.length === 0) {
    return send(res, 200, data.environments[project] ?? []);
  }
  if (kind === "flags") return onFlags(ctx, more);
  return problem(res, 404, "not_found", "No such route.");
}

/** `segments` are what follows `/api/v1`. */
async function api(req: IncomingMessage, res: ServerResponse, segments: string[]) {
  const [projects, project, ...rest] = segments;
  if (projects !== "projects") return problem(res, 404, "not_found", "No such route.");
  if (project === undefined) return send(res, 200, data.projects);
  if (!data.projects.some((p) => p.key === project)) {
    return problem(res, 404, "project_not_found", `No project ${project}.`);
  }
  return onProject({ req, res, project }, rest);
}

createServer((req, res) => {
  const url = new URL(req.url ?? "/", "http://fake");
  if (url.pathname === "/__reset") {
    data = fixture();
    down = false;
    return send(res, 200, { ok: true });
  }
  if (url.pathname === "/__down") {
    down = true;
    return send(res, 200, { ok: true });
  }
  if (url.pathname === "/health") return send(res, 200, { status: "ok" });
  if (down) {
    req.socket.destroy();
    return;
  }
  if (req.headers.authorization !== `Bearer ${E2E_ADMIN_TOKEN}`) {
    return problem(res, 401, "unauthorized", "Missing or wrong admin token.");
  }
  api(req, res, url.pathname.split("/").slice(3).map(decodeURIComponent)).catch(() => {
    problem(res, 500, "internal", "The fake API failed.");
  });
}).listen(API_PORT, "127.0.0.1", () => {
  process.stdout.write(`fake API on http://127.0.0.1:${API_PORT}\n`);
});
