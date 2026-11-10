import type { Case } from "./case.ts";
import { P, config } from "./case.ts";
import { json } from "./fake-server.ts";

const PATH = `${P}/shop/flags/new-checkout/environments/production`;
const SCOPE = ["--project", "shop", "--env", "production"];
/** What `targeting show` prints and `targeting set` sends back. */
export const edited = { ...config, on: false, version: 4 };

export const targetingCases: Case[] = [
  {
    command: "targeting show",
    args: ["targeting", "show", "new-checkout", ...SCOPE],
    routes: { [`GET ${PATH}`]: json(200, config) },
    method: "GET",
    path: PATH,
    shows: ['"on": true', '"version": 4', '"rules": [\n    {\n      "id": "r"\n    }\n  ]'],
    reply: config,
  },
  {
    command: "targeting set",
    args: ["targeting", "set", "new-checkout", ...SCOPE, "--file", "-"],
    stdin: JSON.stringify(edited),
    routes: { [`PUT ${PATH}`]: json(200, { ...edited, version: 5 }) },
    method: "PUT",
    path: PATH,
    body: edited,
    shows: ["saved new-checkout in production at version 5"],
    reply: { ...edited, version: 5 },
  },
];
