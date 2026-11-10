import type { Case } from "./case.ts";
import { P } from "./case.ts";
import { json } from "./fake-server.ts";

export const HASH = "9f2c".repeat(16);

export const snapshot = {
  etag: `"${HASH}"`,
  firstServedAt: "2026-11-05T18:00:00Z",
  flags: [
    {
      key: "new-checkout",
      variations: [true, false],
      on: false,
      offVariation: 1,
      targets: [],
      prerequisites: [],
      rules: [{ clauses: [{ op: "segmentMatch", values: ["beta"], negate: false }], variation: 0 }],
      fallthrough: { variation: 0 },
      version: 4,
    },
  ],
  segments: [{ key: "beta", included: ["u1", "u2"], excluded: [], rules: [] }],
};

export const rulesetCases: Case[] = [
  {
    command: "rulesets show",
    args: ["rulesets", "show", HASH, "--project", "shop", "--env", "production"],
    routes: { [`GET ${P}/shop/environments/production/rulesets/${HASH}`]: json(200, snapshot) },
    method: "GET",
    path: `${P}/shop/environments/production/rulesets/${HASH}`,
    shows: [`"${HASH}"`, "2026-11-05T18:00:00Z", "new-checkout", "false", "beta"],
    reply: snapshot,
  },
];
