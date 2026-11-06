import type { Case } from "./cases.ts";
import { P } from "./cases.ts";
import { json } from "./fake-server.ts";

const environment = { key: "production", name: "Production", createdAt: "2026-11-01T10:05:00Z" };

export const envCases: Case[] = [
  {
    command: "envs rename",
    args: ["envs", "rename", "production", "-p", "shop", "--name", "Production EU"],
    routes: {
      [`PATCH ${P}/shop/environments/production`]: json(200, {
        ...environment,
        name: "Production EU",
      }),
    },
    method: "PATCH",
    path: `${P}/shop/environments/production`,
    body: { name: "Production EU" },
    shows: ["renamed environment production in shop to Production EU"],
    reply: { ...environment, name: "Production EU" },
  },
  {
    command: "envs delete",
    args: ["envs", "delete", "production", "-p", "shop", "--yes"],
    routes: { [`DELETE ${P}/shop/environments/production`]: new Response(null, { status: 204 }) },
    method: "DELETE",
    path: `${P}/shop/environments/production`,
    shows: ["deleted environment production in shop"],
    reply: { key: "production", deleted: true },
  },
];
