import type { Case } from "./case.ts";
import { P } from "./case.ts";
import { json } from "./fake-server.ts";

const BASE = `${P}/shop/environments/production/segments`;
const SCOPE = ["--project", "shop", "--env", "production"];
export const segment = {
  key: "beta",
  included: ["u1", "u2"],
  excluded: ["u3"],
  rules: [{ id: "r" }],
  version: 3,
};

export const segmentCases: Case[] = [
  {
    command: "segments list",
    args: ["segments", "list", ...SCOPE],
    routes: { [`GET ${BASE}`]: json(200, [segment]) },
    method: "GET",
    path: BASE,
    shows: [
      "KEY",
      "INCLUDED",
      "EXCLUDED",
      "RULES",
      "VERSION",
      "beta  2         1         1      3",
    ],
    reply: [segment],
  },
  {
    command: "segments show",
    args: ["segments", "show", "beta", ...SCOPE],
    routes: { [`GET ${BASE}/beta`]: json(200, segment) },
    method: "GET",
    path: `${BASE}/beta`,
    shows: ['"key": "beta"', '"included": [\n    "u1",'],
    reply: segment,
  },
  {
    command: "segments create",
    args: ["segments", "create", "beta", ...SCOPE],
    routes: { [`POST ${BASE}`]: json(201, { ...segment, included: [], excluded: [], rules: [] }) },
    method: "POST",
    path: BASE,
    body: { key: "beta", included: [], excluded: [], rules: [] },
    shows: ["created beta in production"],
    reply: { ...segment, included: [], excluded: [], rules: [] },
  },
  {
    command: "segments set",
    args: ["segments", "set", "beta", ...SCOPE, "--file", "-"],
    stdin: JSON.stringify(segment),
    routes: { [`PUT ${BASE}/beta`]: json(200, { ...segment, version: 4 }) },
    method: "PUT",
    path: `${BASE}/beta`,
    body: { included: ["u1", "u2"], excluded: ["u3"], rules: [{ id: "r" }], version: 3 },
    shows: ["saved beta in production at version 4"],
    reply: { ...segment, version: 4 },
  },
  {
    command: "segments delete",
    args: ["segments", "delete", "beta", ...SCOPE],
    routes: { [`DELETE ${BASE}/beta`]: new Response(null, { status: 204 }) },
    method: "DELETE",
    path: `${BASE}/beta`,
    shows: ["deleted beta from production"],
    reply: { key: "beta", deleted: true },
  },
];
