import type { Admin } from "./admin.ts";
import type { Cli } from "./cli.ts";
import { record, text } from "./data.ts";

export const SEED_PROJECT = "shop";
export const SEED_ENV = "dev";

interface Seeded {
  serverKey: string;
  clientKey: string;
}

/**
 * Fills a fresh server with the shop the example app (`apps/example`) reads: project `shop` with
 * environments `dev` and `prod`, a `pro-customers` segment, five flags across the value types, and
 * a server key and a client key for `dev`. It makes no attempt to be run twice: every create would
 * answer 409 on a database that already has it.
 */
export async function seed(tools: { cli: Cli; admin: Admin }): Promise<Seeded> {
  const { cli, admin } = tools;
  const project = ["--project", SEED_PROJECT];

  await cli.json(["projects", "create", SEED_PROJECT, "--name", "Shop"]);
  await cli.json(["envs", "create", SEED_ENV, ...project, "--name", "Development"]);
  await cli.json(["envs", "create", "prod", ...project, "--name", "Production"]);

  const flag = (key: string, variations: unknown[]) =>
    cli.json(["flags", "create", key, ...project, "--variations", JSON.stringify(variations)]);
  await flag("new-checkout", [true, false]);
  await flag("banner-text", ["Free shipping over $50", "Welcome to the beta!"]);
  await flag("discount-percent", [0, 10, 20]);
  await flag("checkout-config", [
    { layout: "single-page", payment: ["card"] },
    { layout: "multi-step", payment: ["card", "wallet"] },
  ]);
  await flag("legacy-cart", [true, false]); // stays off: a new flag is off in every environment

  await admin.send("POST", `/api/v1/projects/${SEED_PROJECT}/environments/${SEED_ENV}/segments`, {
    key: "pro-customers",
    included: [],
    excluded: [],
    rules: [{ clauses: [{ attribute: "plan", op: "in", values: ["pro"] }] }],
  });

  const configure = async (flagKey: string, config: Record<string, unknown>) => {
    const path = `/api/v1/projects/${SEED_PROJECT}/flags/${flagKey}/environments/${SEED_ENV}`;
    const current = record(await admin.get(path));
    await admin.send("PUT", path, {
      on: true,
      targets: [],
      prerequisites: [],
      rules: [],
      ...config,
      version: current["version"],
    });
  };
  await configure("new-checkout", {
    offVariation: 1,
    rules: [{ clauses: [{ op: "segmentMatch", values: ["pro-customers"] }], variation: 0 }],
    fallthrough: { variation: 1 },
  });
  await configure("banner-text", {
    offVariation: 0,
    targets: [{ variation: 1, contextKeys: ["beta-user"] }],
    fallthrough: { variation: 0 },
  });
  await configure("discount-percent", {
    offVariation: 0,
    fallthrough: {
      rollout: {
        variations: [
          { variation: 0, weight: 50_000 },
          { variation: 1, weight: 30_000 },
          { variation: 2, weight: 20_000 },
        ],
      },
    },
  });
  await configure("checkout-config", { offVariation: 0, fallthrough: { variation: 0 } });

  const key = async (kind: "server" | "client") =>
    text(
      record(
        await cli.json([
          "keys",
          "create",
          ...project,
          "--env",
          SEED_ENV,
          "--kind",
          kind,
          "--name",
          `example-${kind}`,
        ]),
      )["key"],
    );
  return { serverKey: await key("server"), clientKey: await key("client") };
}
