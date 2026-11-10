import type { Case } from "./cases.ts";
import { P, flag } from "./cases.ts";
import { json } from "./fake-server.ts";

export const flagCases: Case[] = [
  {
    command: "flags list",
    args: ["flags", "list", "--project", "shop"],
    routes: { [`GET ${P}/shop/flags`]: json(200, [{ ...flag, lifecycle: "inactive" }]) },
    method: "GET",
    path: `${P}/shop/flags`,
    shows: ["new-checkout", "inactive"],
    reply: [{ ...flag, lifecycle: "inactive" }],
  },
  {
    command: "flags delete",
    args: ["flags", "delete", "new-checkout", "--project", "shop"],
    routes: { [`DELETE ${P}/shop/flags/new-checkout`]: new Response(null, { status: 204 }) },
    method: "DELETE",
    path: `${P}/shop/flags/new-checkout`,
    shows: ["deleted new-checkout"],
    reply: { key: "new-checkout", deleted: true },
  },
];
