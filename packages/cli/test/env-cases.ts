import type { Case } from "./case.ts";
import { P } from "./case.ts";
import { json } from "./fake-server.ts";

const environment = {
  key: "production",
  name: "Production",
  createdAt: "2026-11-01T10:05:00Z",
  secureMode: false,
  signingKeyCreatedAt: null,
};
const pem = "-----BEGIN PRIVATE KEY-----\nPLAINPRIVATEKEY\n-----END PRIVATE KEY-----\n";

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
    command: "envs signing-key",
    args: ["envs", "signing-key", "production", "-p", "shop"],
    routes: {
      [`POST ${P}/shop/environments/production/signing-key`]: json(201, {
        privateKey: pem,
        createdAt: "2026-11-15T08:00:00Z",
      }),
    },
    method: "POST",
    path: `${P}/shop/environments/production/signing-key`,
    shows: [pem],
    reply: { privateKey: pem, createdAt: "2026-11-15T08:00:00Z" },
  },
  {
    command: "envs secure-mode",
    args: ["envs", "secure-mode", "production", "on", "-p", "shop"],
    routes: {
      [`PATCH ${P}/shop/environments/production`]: json(200, {
        ...environment,
        secureMode: true,
        signingKeyCreatedAt: "2026-11-15T08:00:00Z",
      }),
    },
    method: "PATCH",
    path: `${P}/shop/environments/production`,
    body: { secureMode: true },
    shows: ["secure mode is on for environment production in shop"],
    reply: { ...environment, secureMode: true, signingKeyCreatedAt: "2026-11-15T08:00:00Z" },
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
