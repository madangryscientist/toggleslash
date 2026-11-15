/** Plain data in the shapes the dashboard's server functions answer (`#/api/types`), for DOM tests. */
import type {
  ApiKey,
  DetailLoaded,
  Environment,
  FlagDetail,
  FlagRow,
  Loaded,
  Project,
  Segment,
} from "#/api/types";
import type { Config } from "#/targeting/model";

export const shop: Project = { key: "shop", name: "Shop" };
export const docs: Project = { key: "docs", name: "Docs" };

export function environment(
  key: string,
  name: string,
  more: Partial<Environment> = {},
): Environment {
  return {
    key,
    name,
    createdAt: "2026-11-01T09:00:00Z",
    secureMode: false,
    signingKeyCreatedAt: null,
    ...more,
  };
}

export const production = environment("production", "Production");
export const staging = environment("staging", "Staging");

export const checkout: FlagDetail = {
  key: "checkout-v2",
  name: "Checkout v2",
  description: "",
  owner: "",
  tags: ["checkout"],
  temporary: true,
  clientVisible: false,
  archived: false,
  lifecycle: "active",
  variations: ["true", "false"],
  lastEvaluated: "2026-11-04",
};

/** checkout-v2 in production: a target, a rule that rolls out 25/75, and a fallthrough. */
export const checkoutConfig: Config = {
  on: true,
  offVariation: 1,
  targets: [{ variation: 0, contextKeys: ["alice", "bob"] }],
  prerequisites: [],
  rules: [
    {
      clauses: [{ op: "in", attribute: "country", values: ["NL", "DE"], negate: false }],
      serve: {
        kind: "rollout",
        entries: [
          { variation: 0, weight: 25_000 },
          { variation: 1, weight: 75_000 },
        ],
      },
    },
  ],
  fallthrough: { kind: "variation", variation: 1 },
  version: 3,
};

export const darkMode: FlagDetail = {
  ...checkout,
  key: "dark-mode",
  name: "Dark mode",
  tags: ["ui"],
  temporary: false,
  lifecycle: "new",
  lastEvaluated: null,
};

/** What `loadFlagDetail` answers for checkout-v2 in production, at `config`'s version. */
export function checkoutDetail(
  config: Config = checkoutConfig,
): Extract<DetailLoaded, { ok: true }> {
  return {
    ok: true,
    project: shop,
    environments: [production, staging],
    environment: production,
    flag: checkout,
    config,
    siblings: [{ key: "dark-mode", name: "Dark mode", variations: ["true", "false"] }],
    history: [],
    usage: { environments: [] },
  };
}

export function row(key: string, name: string, more: Partial<FlagRow> = {}): FlagRow {
  return {
    key,
    name,
    description: "",
    tags: [],
    temporary: false,
    lifecycle: "new",
    lastEvaluated: null,
    on: false,
    version: 1,
    ...more,
  };
}

export const shopRows: FlagRow[] = [
  row("checkout-v2", "Checkout v2", {
    tags: ["checkout"],
    temporary: true,
    lifecycle: "active",
    lastEvaluated: "2026-11-04",
    on: true,
    version: 3,
  }),
  row("dark-mode", "Dark mode", { tags: ["ui"] }),
];

/** What `loadDashboard` answers for shop in production, with `flags` as its rows. */
export function dashboard(
  flags: FlagRow[] = shopRows,
  archived = false,
): Extract<Loaded, { ok: true }> {
  return {
    ok: true,
    projects: [shop, docs],
    project: shop,
    environments: [production, staging],
    environment: production,
    flags,
    archived,
  };
}

/** The project and environment part every operator page's loader answers. */
export function scope(env: Environment | null = production) {
  return {
    ok: true as const,
    projects: [shop, docs],
    project: shop,
    environments: [production, staging],
    environment: env,
  };
}

export function apiKey(id: number, name: string, more: Partial<ApiKey> = {}): ApiKey {
  return {
    id,
    kind: "server",
    name,
    prefix: `tgl_srv_a1b2c3d${id}`,
    createdAt: "2026-11-01T09:00:00Z",
    revokedAt: null,
    ...more,
  };
}

export function segment(key: string, more: Partial<Segment> = {}): Segment {
  return {
    key,
    included: [],
    excluded: [],
    includedContexts: [],
    excludedContexts: [],
    rules: "[]",
    version: 1,
    ...more,
  };
}

export const betaTesters = segment("beta-testers", {
  included: ["alice", "bob"],
  excluded: ["mallory"],
  rules: '[{"clauses":[{"attribute":"plan","op":"in","values":["beta"]}]}]',
});
export const staff = segment("staff");
