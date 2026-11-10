import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { createAdmin, type Admin } from "../src/admin.ts";
import { createCli, type Cli } from "../src/cli.ts";
import { record } from "../src/data.ts";
import { skipReason } from "../src/dotnet.ts";
import { startExample, type RunningExample } from "../src/example.ts";
import { seed, SEED_ENV, SEED_PROJECT } from "../src/seed.ts";
import { buildServer, startServer, type RunningServer } from "../src/server.ts";
import { until } from "../src/wait.ts";

/**
 * The seeded shop and the example app (`apps/example`) against the real server, with the real CLI
 * and the real SDK inside the app: no browser and no dashboard.
 */
const skip = skipReason() !== null;

const ADMIN_TOKEN = "e2e-example-throwaway-admin-token";

interface State {
  user: { key: string };
  flags: { key: string; value: unknown; reason: { kind: string } }[];
  status: { etag: string | null; stream: string; lastError: string | null };
}

const valueOf = (s: State, flag: string): unknown => s.flags.find((f) => f.key === flag)?.value;

describe.skipIf(skip)("the example app, the seed and the real server", () => {
  let scratch = "";
  let server: RunningServer | null = null;
  let app: RunningExample | null = null;
  let cli: Cli;
  let admin: Admin;

  const appUrl = (): string => app?.url ?? "";
  async function state(user: string): Promise<State> {
    const response = await fetch(`${appUrl()}/state?user=${user}`);
    expect(response.status).toBe(200);
    const body: State = JSON.parse(await response.text());
    return body;
  }
  const runtimeEtag = async (): Promise<string> =>
    String(
      record(await cli.json(["runtime", "--project", SEED_PROJECT, "--env", SEED_ENV]))["etag"],
    );

  beforeAll(async () => {
    scratch = mkdtempSync(join(tmpdir(), "toggleslash-example-e2e-"));
    mkdirSync(join(scratch, "home"));
    const assembly = await buildServer();
    server = await startServer(assembly, {
      dbPath: join(scratch, "toggleslash.db"),
      adminToken: ADMIN_TOKEN,
    });
    cli = createCli({ url: server.url, token: ADMIN_TOKEN, home: join(scratch, "home") });
    admin = createAdmin({ url: server.url, token: ADMIN_TOKEN });
    const keys = await seed({ cli, admin });
    app = await startExample({
      serverUrl: server.url,
      sdkKey: keys.serverKey,
      clientKey: keys.clientKey,
      port: 0,
      flushIntervalMs: 200, // usage reaches the server in a fraction of a second
      tmpdir: scratch,
    });
    await until(
      "the app's SDK to load the ruleset",
      async () => (await state("free-user")).status.etag !== null,
    );
  });

  afterAll(async () => {
    try {
      await app?.proc.stop();
    } finally {
      try {
        await server?.stop();
      } finally {
        rmSync(scratch, { recursive: true, force: true });
      }
    }
  });

  it("serves a different new-checkout to a free user and a pro user, and every flag from the server", async () => {
    const free = await state("free-user");
    const pro = await state("pro-user");
    expect(valueOf(free, "new-checkout")).toBe(false);
    expect(valueOf(pro, "new-checkout")).toBe(true);
    expect(pro.flags.find((f) => f.key === "new-checkout")?.reason.kind).toBe("RULE_MATCH");

    // Every flag the app reads exists on the server: none is the app's fallback.
    for (const s of [free, pro]) {
      expect(s.flags.map((f) => f.reason.kind)).not.toContain("ERROR");
    }
    expect(valueOf(await state("beta-user"), "banner-text")).toBe("Welcome to the beta!");
    expect(valueOf(free, "banner-text")).toBe("Free shipping over $50");
    expect(valueOf(free, "legacy-cart")).toBe(false);
    expect(valueOf(free, "checkout-config")).toEqual({ layout: "single-page", payment: ["card"] });
    expect([0, 10, 20]).toContain(valueOf(free, "discount-percent"));
  });

  it("reports the ruleset the server is serving, and a connected stream", async () => {
    await until(
      "the stream to connect",
      async () => (await state("free-user")).status.stream === "connected",
    );
    const status = (await state("free-user")).status;
    expect(status.lastError).toBeNull();
    expect(status.etag).toBe(await runtimeEtag());
  });

  it("sees a CLI flip through the stream and then reports the new ruleset's tag", async () => {
    const before = (await state("free-user")).status.etag;
    const on = await cli.run([
      "flags",
      "on",
      "legacy-cart",
      "--project",
      SEED_PROJECT,
      "--env",
      SEED_ENV,
    ]);
    expect(on.code).toBe(0);
    await until(
      "legacy-cart to be true in /state",
      async () => valueOf(await state("free-user"), "legacy-cart") === true,
    );
    const after = (await state("free-user")).status.etag;
    expect(after).not.toBe(before);
    expect(after).toBe(await runtimeEtag());
  });

  it("sends usage counts to the server after a flush", async () => {
    await state("pro-user"); // serves new-checkout at least once
    let total = 0;
    await until("usage counts to arrive", async () => {
      const usage = record(
        await admin.get(`/api/v1/projects/${SEED_PROJECT}/flags/new-checkout/usage`),
      );
      const environments = usage["environments"];
      const first = Array.isArray(environments) ? record(environments[0]) : null;
      total = Number(first?.["total"] ?? 0);
      return total > 0;
    });
    expect(total).toBeGreaterThan(0);
  });

  it("answers a client-key evaluate from the browser, and serves the bundle that makes it", async () => {
    const html = await (await fetch(`${appUrl()}/?user=pro-user`)).text();
    const key = /fs_client_[A-Za-z0-9_-]+/.exec(html)?.[0];
    expect(key).toBeDefined();
    const response = await fetch(`${server?.url}/sdk/v1/evaluate`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${key}`,
        "Content-Type": "application/json",
        Origin: appUrl(),
      },
      body: JSON.stringify({ context: { key: "pro-user", attributes: { plan: "pro" } } }),
    });
    expect(response.status).toBe(200);
    expect(response.headers.get("Access-Control-Allow-Origin")).not.toBeNull();
    const flags = record(record(await response.json())["flags"]);
    expect(record(flags["new-checkout"])["value"]).toBe(true);
    expect(html).not.toContain("fs_server_");

    const bundle = await fetch(`${appUrl()}/browser.js`);
    expect(bundle.status).toBe(200);
  });
});
