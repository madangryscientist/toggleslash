import type { Handler } from "./fake-server.ts";
import { json } from "./fake-server.ts";

export const P = "/api/v1/projects";
const project = { key: "shop", name: "Shop", createdAt: "2026-11-01T10:00:00Z" };
const environment = { key: "production", name: "Production", createdAt: "2026-11-01T10:05:00Z" };
export const flag = {
  key: "new-checkout",
  name: "New checkout",
  description: "",
  variations: [true, false],
  temporary: false,
  owner: null,
  tags: ["checkout"],
  createdAt: "2026-11-01T11:00:00Z",
  archivedAt: null,
  lastEvaluated: "2026-11-03",
  lifecycle: "active",
};
export const config = {
  on: true,
  offVariation: 1,
  targets: [],
  prerequisites: [],
  rules: [{ id: "r" }],
  fallthrough: { variation: 0 },
  version: 4,
};
const keySummary = {
  id: 7,
  kind: "server",
  name: "ci",
  prefix: "fs_server_ab12",
  createdAt: "2026-11-02T08:00:00Z",
  revokedAt: null,
};
const historyPage = {
  events: [
    {
      id: 3,
      occurredAt: "2026-11-03T09:00:00Z",
      actor: "admin",
      entityType: "flag_config",
      entityId: "new-checkout/production",
      action: "toggled",
      project: "shop",
      environment: "production",
      flag: "new-checkout",
      before: null,
      after: null,
    },
  ],
  nextCursor: "c-2",
};

export const diagnosis = {
  flags: { "new-checkout": { value: true, variationIndex: 0, reason: { kind: "FALLTHROUGH" } } },
};

export interface Case {
  /** The command's name, so a test below can prove no command is untested. */
  command: string;
  args: string[];
  routes: Record<string, Handler | Response>;
  method: string;
  path: string;
  query?: string;
  body?: unknown;
  /** Text the human output must contain. */
  shows: string[];
  /** The reply that `--json` must print back as it was (reads and writes alike). */
  reply: unknown;
}

export const cases: Case[] = [
  {
    command: "projects list",
    args: ["projects", "list"],
    routes: { [`GET ${P}`]: json(200, [project]) },
    method: "GET",
    path: P,
    shows: ["KEY", "shop", "Shop"],
    reply: [project],
  },
  {
    command: "projects create",
    args: ["projects", "create", "shop", "--name", "Shop"],
    routes: { [`POST ${P}`]: json(201, project) },
    method: "POST",
    path: P,
    body: { key: "shop", name: "Shop" },
    shows: ["created project shop"],
    reply: project,
  },
  {
    command: "envs list",
    args: ["envs", "list", "--project", "shop"],
    routes: { [`GET ${P}/shop/environments`]: json(200, [environment]) },
    method: "GET",
    path: `${P}/shop/environments`,
    shows: ["production", "Production"],
    reply: [environment],
  },
  {
    command: "envs create",
    args: ["envs", "create", "production", "-p", "shop"],
    routes: { [`POST ${P}/shop/environments`]: json(201, environment) },
    method: "POST",
    path: `${P}/shop/environments`,
    body: { key: "production", name: "production" },
    shows: ["created environment production in shop"],
    reply: environment,
  },
  {
    command: "flags list",
    args: ["flags", "list", "--project", "shop", "--tag", "checkout", "--archived", "all"],
    routes: { [`GET ${P}/shop/flags`]: json(200, [flag]) },
    method: "GET",
    path: `${P}/shop/flags`,
    query: "?tag=checkout&archived=all",
    shows: ["new-checkout", "New checkout", "[true,false]", "checkout", "active", "2026-11-03"],
    reply: [flag],
  },
  {
    command: "flags show",
    args: ["flags", "show", "new-checkout", "--project", "shop"],
    routes: { [`GET ${P}/shop/flags/new-checkout`]: json(200, flag) },
    method: "GET",
    path: `${P}/shop/flags/new-checkout`,
    shows: ["key:", "new-checkout", "variations:", "[true,false]"],
    reply: flag,
  },
  {
    command: "flags create",
    args: [
      "flags",
      "create",
      "new-checkout",
      "--project",
      "shop",
      "--name",
      "New checkout",
      "--variations",
      '["a","b","c"]',
      "--description",
      "d",
      "--temporary",
      "--owner",
      "me",
      "--tag",
      "x",
      "--tag",
      "y",
      "--off-variation",
      "2",
      "--fallthrough-variation",
      "1",
    ],
    routes: { [`POST ${P}/shop/flags`]: json(201, flag) },
    method: "POST",
    path: `${P}/shop/flags`,
    body: {
      key: "new-checkout",
      name: "New checkout",
      variations: ["a", "b", "c"],
      description: "d",
      temporary: true,
      owner: "me",
      tags: ["x", "y"],
      offVariation: 2,
      fallthroughVariation: 1,
    },
    shows: ["created flag new-checkout in shop"],
    reply: flag,
  },
  {
    command: "flags archive",
    args: ["flags", "archive", "new-checkout", "--project", "shop"],
    routes: {
      [`POST ${P}/shop/flags/new-checkout/archive`]: json(200, {
        ...flag,
        archivedAt: "2026-11-04T00:00:00Z",
      }),
    },
    method: "POST",
    path: `${P}/shop/flags/new-checkout/archive`,
    shows: ["archived flag new-checkout in shop"],
    reply: { ...flag, archivedAt: "2026-11-04T00:00:00Z" },
  },
  {
    command: "flags on",
    args: ["flags", "on", "new-checkout", "--project", "shop", "--env", "production"],
    routes: {
      [`POST ${P}/shop/flags/new-checkout/environments/production/toggle`]: json(200, config),
    },
    method: "POST",
    path: `${P}/shop/flags/new-checkout/environments/production/toggle`,
    body: { on: true },
    shows: ["flag new-checkout is on in production (version 4)"],
    reply: config,
  },
  {
    command: "flags off",
    args: ["flags", "off", "new-checkout", "-p", "shop", "-e", "production"],
    routes: {
      [`POST ${P}/shop/flags/new-checkout/environments/production/toggle`]: json(200, {
        ...config,
        on: false,
        version: 5,
      }),
    },
    method: "POST",
    path: `${P}/shop/flags/new-checkout/environments/production/toggle`,
    body: { on: false },
    shows: ["flag new-checkout is off in production (version 5)"],
    reply: { ...config, on: false, version: 5 },
  },
  {
    command: "flags history",
    args: [
      "flags",
      "history",
      "new-checkout",
      "--project",
      "shop",
      "--env",
      "production",
      "--limit",
      "10",
      "--since",
      "2026-11-01",
      "--cursor",
      "c-1",
    ],
    routes: { [`GET ${P}/shop/flags/new-checkout/history`]: json(200, historyPage) },
    method: "GET",
    path: `${P}/shop/flags/new-checkout/history`,
    query: "?environment=production&since=2026-11-01&limit=10&cursor=c-1",
    shows: ["toggled", "flag_config new-checkout/production", "more: add --cursor c-2"],
    reply: historyPage,
  },
  {
    command: "keys create",
    args: [
      "keys",
      "create",
      "--project",
      "shop",
      "--env",
      "production",
      "--kind",
      "server",
      "--name",
      "ci",
    ],
    routes: {
      [`POST ${P}/shop/environments/production/keys`]: json(201, {
        ...keySummary,
        key: "fs_server_PLAINKEY",
      }),
    },
    method: "POST",
    path: `${P}/shop/environments/production/keys`,
    body: { kind: "server", name: "ci" },
    shows: ["fs_server_PLAINKEY", "ci"],
    reply: { ...keySummary, key: "fs_server_PLAINKEY" },
  },
  {
    command: "keys list",
    args: ["keys", "list", "--project", "shop", "--env", "production"],
    routes: { [`GET ${P}/shop/environments/production/keys`]: json(200, [keySummary]) },
    method: "GET",
    path: `${P}/shop/environments/production/keys`,
    shows: ["fs_server_ab12", "ci", "server"],
    reply: [keySummary],
  },
  {
    command: "keys revoke",
    args: ["keys", "revoke", "7", "--project", "shop", "--env", "production"],
    routes: {
      [`DELETE ${P}/shop/environments/production/keys/7`]: new Response(null, { status: 204 }),
    },
    method: "DELETE",
    path: `${P}/shop/environments/production/keys/7`,
    shows: ["revoked key 7"],
    reply: { id: 7, revoked: true },
  },
  {
    command: "diagnose",
    args: [
      "diagnose",
      "new-checkout",
      "--project",
      "shop",
      "--env",
      "production",
      "--context",
      '{"key":"u1"}',
    ],
    routes: { [`POST ${P}/shop/environments/production/evaluate`]: json(200, diagnosis) },
    method: "POST",
    path: `${P}/shop/environments/production/evaluate`,
    body: { flagKey: "new-checkout", context: { key: "u1" } },
    shows: ["value:", "true", "variationIndex:", "0", "reason:", "FALLTHROUGH"],
    reply: diagnosis,
  },
];
