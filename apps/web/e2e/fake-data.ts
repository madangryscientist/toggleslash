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

export interface Store {
  projects: Named[];
  environments: Record<string, Named[]>;
  flags: Record<string, Flag[]>;
  /** `project/env/flag` -> config */
  configs: Record<string, Config>;
  /** `project/flag` -> newest first */
  history: Record<string, AuditEvent[]>;
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
