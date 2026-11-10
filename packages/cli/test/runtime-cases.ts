import type { Case } from "./case.ts";
import { P } from "./case.ts";
import { json } from "./fake-server.ts";

export const runtime = {
  etag: '"9f2c"',
  flagCount: 12,
  generatedAt: "2026-11-05T18:00:00Z",
  lastChange: {
    at: "2026-11-05T17:58:00Z",
    actor: "token:3",
    entityType: "flag_config",
    entityId: "new-checkout/production",
    action: "toggled",
  },
};

export const runtimeCases: Case[] = [
  {
    command: "runtime",
    args: ["runtime", "--project", "shop", "--env", "production"],
    routes: { [`GET ${P}/shop/environments/production/runtime`]: json(200, runtime) },
    method: "GET",
    path: `${P}/shop/environments/production/runtime`,
    shows: ['"9f2c"', "flagCount:", "12", "token:3", "toggled", "new-checkout/production"],
    reply: runtime,
  },
];
