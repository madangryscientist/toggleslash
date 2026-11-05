/** The fake API's in-memory data: the types, the fixture and the one mutable `db`. */

export interface Flag {
  key: string;
  name: string;
  description: string;
  variations: unknown[];
  temporary: boolean;
  owner: string | null;
  tags: string[];
  createdAt: string;
  archivedAt: string | null;
  lastEvaluated: string | null;
}

export interface Config {
  on: boolean;
  offVariation: number | null;
  targets: unknown[];
  prerequisites: unknown[];
  rules: unknown[];
  fallthrough: Record<string, unknown>;
  version: number;
}

export interface Named {
  key: string;
  name: string;
  createdAt: string;
}

export interface AuditEvent {
  id: number;
  occurredAt: string;
  actor: string;
  entityType: string;
  action: string;
  environment: string | null;
  after: { version: number } | null;
}

export interface KeyRow {
  id: number;
  kind: "server" | "client";
  name: string;
  prefix: string;
  createdAt: string;
  revokedAt: string | null;
}

export interface SegmentRow {
  key: string;
  included: string[];
  excluded: string[];
  rules: unknown[];
  version: number;
}

/** A project audit event, as `GET /projects/<p>/audit` returns it. */
interface ProjectEvent {
  id: number;
  occurredAt: string;
  actor: string;
  entityType: string;
  entityId: string;
  action: string;
  project: string;
  environment: string | null;
  flag: string | null;
}

export interface Store {
  projects: Named[];
  environments: Record<string, Named[]>;
  flags: Record<string, Flag[]>;
  /** `project/env/flag` -> config */
  configs: Record<string, Config>;
  /** `project/env/flag` -> the latest day that environment counted an evaluation; no entry means never */
  lastEvaluated: Record<string, string>;
  /** `project/flag` -> newest first */
  history: Record<string, AuditEvent[]>;
  /** `project/env` -> keys, oldest first */
  keys: Record<string, KeyRow[]>;
  /** `project/env` -> segments */
  segments: Record<string, SegmentRow[]>;
  /** `project` -> newest first */
  audit: Record<string, ProjectEvent[]>;
}

interface Seed {
  key: string;
  name: string;
  tags?: string[];
  temporary?: boolean;
  description?: string;
  last?: string | null;
}

export function flag(seed: Seed): Flag {
  return {
    key: seed.key,
    name: seed.name,
    description: seed.description ?? "",
    variations: [true, false],
    temporary: seed.temporary ?? false,
    owner: null,
    tags: seed.tags ?? [],
    createdAt: "2026-11-01T09:00:00Z",
    archivedAt: null,
    lastEvaluated: seed.last ?? null,
  };
}

function named(key: string, name: string): Named {
  return { key, name, createdAt: "2026-11-01T09:00:00Z" };
}

/** A config that is just on or off, with nothing else set. */
export function plain(on: boolean, version: number): Config {
  return {
    on,
    offVariation: 1,
    targets: [],
    prerequisites: [],
    rules: [],
    fallthrough: { variation: on ? 0 : 1 },
    version,
  };
}

/** checkout-v2 in production: a target, a rule that rolls out 25/75, and a fallthrough. */
function checkoutConfig(): Config {
  return {
    on: true,
    offVariation: 1,
    targets: [{ variation: 0, contextKeys: ["alice", "bob"] }],
    prerequisites: [],
    rules: [
      {
        clauses: [{ op: "in", attribute: "country", values: ["NL", "DE"] }],
        rollout: {
          variations: [
            { variation: 0, weight: 25000 },
            { variation: 1, weight: 75000 },
          ],
        },
      },
    ],
    fallthrough: { variation: 1 },
    version: 3,
  };
}

function event(id: number, action: string, at: string, version: number | null): AuditEvent {
  return {
    id,
    occurredAt: at,
    actor: "admin",
    entityType: version === null ? "flag" : "flag_config",
    action,
    environment: version === null ? null : "production",
    after: version === null ? null : { version },
  };
}

/** Thirty events, one a day through October, so the log has a second page at 25 per page. */
function shopAudit(): ProjectEvent[] {
  const events: ProjectEvent[] = [];
  for (let i = 1; i <= 30; i++) {
    const flagKey = [null, "dark-mode", "checkout-v2"][i % 3] ?? null;
    events.push({
      id: i,
      occurredAt: `2026-10-${String(i).padStart(2, "0")}T12:00:00Z`,
      actor: "admin",
      entityType: flagKey === null ? "environment" : "flag_config",
      entityId: flagKey ?? "staging",
      action: flagKey === null ? "environment.created" : "config.updated",
      project: "shop",
      environment: i % 2 === 0 ? "production" : "staging",
      flag: flagKey,
    });
  }
  return events.toReversed();
}

/** Production's days are the flags' own `last` (the latest in any environment); staging's are older. */
function lastDays(): Record<string, string> {
  return {
    "shop/production/checkout-v2": "2026-11-04",
    "shop/production/search-boost": "2026-10-30",
    "shop/staging/checkout-v2": "2026-11-02",
    "shop/staging/search-boost": "2026-10-28",
  };
}

export function fixture(): Store {
  return {
    projects: [named("shop", "Shop"), named("docs", "Docs")],
    environments: {
      shop: [named("production", "Production"), named("staging", "Staging")],
      docs: [named("live", "Live")],
    },
    flags: {
      shop: [
        flag({
          key: "checkout-v2",
          name: "Checkout v2",
          tags: ["checkout"],
          temporary: true,
          last: "2026-11-04",
        }),
        flag({ key: "dark-mode", name: "Dark mode", tags: ["ui"] }),
        flag({
          key: "search-boost",
          name: "Search boost",
          tags: ["search", "ui"],
          last: "2026-10-30",
        }),
      ],
      docs: [flag({ key: "docs-search", name: "Docs search" })],
    },
    configs: {
      "shop/production/checkout-v2": checkoutConfig(),
      "shop/production/dark-mode": plain(false, 1),
      "shop/production/search-boost": plain(false, 2),
      "shop/staging/checkout-v2": plain(true, 1),
      "shop/staging/dark-mode": plain(true, 1),
      "shop/staging/search-boost": plain(true, 1),
      "docs/live/docs-search": plain(false, 1),
    },
    lastEvaluated: lastDays(),
    keys: {
      "shop/production": [
        {
          id: 1,
          kind: "server",
          name: "Backend",
          prefix: "fs_srv_a1b2c3d4",
          createdAt: "2026-11-01T09:00:00Z",
          revokedAt: null,
        },
        {
          id: 2,
          kind: "client",
          name: "Old web",
          prefix: "fs_cli_e5f6a7b8",
          createdAt: "2026-11-01T09:30:00Z",
          revokedAt: "2026-11-03T10:00:00Z",
        },
      ],
    },
    segments: {
      "shop/production": [
        {
          key: "beta-testers",
          included: ["alice", "bob"],
          excluded: ["mallory"],
          rules: [{ clauses: [{ op: "in", attribute: "country", values: ["NL"] }] }],
          version: 1,
        },
        { key: "staff", included: [], excluded: [], rules: [], version: 1 },
      ],
    },
    audit: { shop: shopAudit() },
    history: {
      "shop/checkout-v2": [
        event(3, "config.updated", "2026-11-04T10:00:00Z", 3),
        event(2, "config.updated", "2026-11-03T10:00:00Z", 2),
        event(1, "flag.created", "2026-11-01T09:00:00Z", null),
      ],
    },
  };
}

export const db = { data: fixture(), down: false };

export function reset() {
  db.data = fixture();
  db.down = false;
}
