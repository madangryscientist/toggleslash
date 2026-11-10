import type { Case } from "./case.ts";
import { P } from "./case.ts";
import { json } from "./fake-server.ts";

const tokenSummary = {
  id: 9,
  name: "terraform",
  permission: "write",
  prefix: "fs_admin_ab12",
  createdAt: "2026-11-02T08:00:00Z",
  expiresAt: "2027-02-02T08:00:00Z",
  revokedAt: null,
};

export const tokenCases: Case[] = [
  {
    command: "tokens create",
    args: [
      "tokens",
      "create",
      "--project",
      "shop",
      "--name",
      "terraform",
      "--permission",
      "write",
      "--expires-in-days",
      "90",
    ],
    routes: {
      [`POST ${P}/shop/tokens`]: json(201, { ...tokenSummary, token: "fs_admin_PLAINTOKEN" }),
    },
    method: "POST",
    path: `${P}/shop/tokens`,
    body: { name: "terraform", permission: "write", expiresInDays: 90 },
    shows: ["fs_admin_PLAINTOKEN", "terraform", "write"],
    reply: { ...tokenSummary, token: "fs_admin_PLAINTOKEN" },
  },
  {
    command: "tokens list",
    args: ["tokens", "list", "--project", "shop"],
    routes: { [`GET ${P}/shop/tokens`]: json(200, [tokenSummary]) },
    method: "GET",
    path: `${P}/shop/tokens`,
    shows: ["fs_admin_ab12", "terraform", "write", "2027-02-02T08:00:00Z"],
    reply: [tokenSummary],
  },
  {
    command: "tokens revoke",
    args: ["tokens", "revoke", "9", "--project", "shop"],
    routes: { [`DELETE ${P}/shop/tokens/9`]: new Response(null, { status: 204 }) },
    method: "DELETE",
    path: `${P}/shop/tokens/9`,
    shows: ["revoked token 9"],
    reply: { id: 9, revoked: true },
  },
];
