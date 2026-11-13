import type { Handler } from "./fake-server.ts";

export const P = "/api/v1/projects";
export const project = { key: "shop", name: "Shop", createdAt: "2026-11-01T10:00:00Z" };
export const environment = {
  key: "production",
  name: "Production",
  createdAt: "2026-11-01T10:05:00Z",
};
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
export const keySummary = {
  id: 7,
  kind: "server",
  name: "ci",
  prefix: "tgl_server_ab12",
  createdAt: "2026-11-02T08:00:00Z",
  revokedAt: null,
};
export const historyPage = {
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
  /** Piped to stdin, for a command that reads `--file -`. */
  stdin?: string;
  /** The reply that `--json` must print back as it was (reads and writes alike). */
  reply: unknown;
}
