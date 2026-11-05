import { createClient, type Client, type Fetch } from "@hackcove/featureslash-sdk";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { createAdmin, type Admin } from "../src/admin.ts";
import { createCli, type Cli } from "../src/cli.ts";
import { contexts, record, text } from "../src/data.ts";
import { FLAGS, SEGMENT } from "../src/flags.ts";
import { skipReason } from "../src/dotnet.ts";
import { buildServer, startServer, type RunningServer } from "../src/server.ts";
import { until } from "../src/wait.ts";

/**
 * The proof the whole product runs: the real C# server (built here, on a port the OS picks, with a
 * SQLite file under TMPDIR), the real CLI as a subprocess and the real TypeScript SDK. Loopback only.
 */
const skip = skipReason() !== null;

const ADMIN_TOKEN = "e2e-throwaway-admin-token-not-a-secret";
const PROJECT = "shop";
const ENV = "production";
// A null fallback accepts any served type; a typed one would turn a mismatch into the fallback.
const FALLBACK = null;

/** `rulesets show` for one hash, as the ETag gives it, narrowed to the flag these tests flip. */
const show = (hash: string): string[] => [
  "rulesets",
  "show",
  hash.replaceAll('"', ""),
  "--project",
  PROJECT,
  "--env",
  ENV,
  "--flag",
  "new-checkout",
];

const flagIn = (answer: unknown): Record<string, unknown> => {
  const flags = record(answer)["flags"];
  return record(Array.isArray(flags) ? flags[0] : undefined);
};

describe.skipIf(skip)("server, CLI and SDK end to end", () => {
  let scratch = "";
  let server: RunningServer | null = null;
  let client: Client | null = null;
  let cli: Cli;
  let admin: Admin;
  let serverKey = "";
  let clientKey = "";
  const sdkErrors: unknown[] = [];
  const requests: { path: string; etag: string | null }[] = [];

  /** The SDK's own fetch, wrapped to see which routes it used. */
  const watchedFetch: Fetch = async (input, init) => {
    const response = await fetch(input, init);
    requests.push({ path: new URL(input).pathname, etag: response.headers.get("ETag") });
    return response;
  };

  const sdk = (): Client => {
    if (client === null) throw new Error("the SDK client is not created yet");
    return client;
  };
  const used = (path: string) => requests.filter((r) => r.path === path).length;
  const lastTag = () => requests.findLast((r) => r.path === "/sdk/v1/ruleset")?.etag ?? "";

  beforeAll(async () => {
    // TMPDIR is a scoped directory for the run (scripts/test-tmp-scope.ts); it is removed after it.
    scratch = mkdtempSync(join(tmpdir(), "featureslash-e2e-"));
    mkdirSync(join(scratch, "home"));
    const assembly = await buildServer();
    server = await startServer(assembly, {
      dbPath: join(scratch, "featureslash.db"),
      adminToken: ADMIN_TOKEN,
    });
    cli = createCli({ url: server.url, token: ADMIN_TOKEN, home: join(scratch, "home") });
    admin = createAdmin({ url: server.url, token: ADMIN_TOKEN });
  });

  afterAll(async () => {
    try {
      await client?.close();
    } finally {
      try {
        await server?.stop();
      } finally {
        rmSync(scratch, { recursive: true, force: true });
      }
    }
  });

  it("creates a project, an environment, a flag and SDK keys with the CLI", async () => {
    await cli.json(["projects", "create", PROJECT, "--name", "Shop"]);
    await cli.json(["envs", "create", ENV, "--project", PROJECT]);
    await cli.json(["flags", "create", "new-checkout", "--project", PROJECT]);

    const serverCreated = record(
      await cli.json([
        "keys",
        "create",
        "--project",
        PROJECT,
        "--env",
        ENV,
        "--kind",
        "server",
        "--name",
        "e2e-server",
      ]),
    );
    const clientCreated = record(
      await cli.json([
        "keys",
        "create",
        "--project",
        PROJECT,
        "--env",
        ENV,
        "--kind",
        "client",
        "--name",
        "e2e-client",
      ]),
    );
    serverKey = text(serverCreated["key"]);
    clientKey = text(clientCreated["key"]);
    expect(serverKey).toMatch(/^fs_server_/);
    expect(clientKey).toMatch(/^fs_client_/);

    const flag = record(
      await cli.json(["flags", "show", "new-checkout", "--project", PROJECT, "--env", ENV]),
    );
    expect(flag["key"]).toBe("new-checkout");
  });

  it("lets the SDK read the flag", async () => {
    // A long poll interval: any change seen within the test's seconds came through the stream.
    client = createClient({
      url: server?.url ?? "",
      sdkKey: serverKey,
      pollIntervalMs: 600_000,
      fetch: watchedFetch,
      onError: (error) => sdkErrors.push(error),
    });
    expect(await sdk().ready()).toBe(true);
    const detail = sdk().variationDetail("new-checkout", { key: "someone" }, FALLBACK);
    expect(detail.value).toBe(false);
    expect(detail.reason.kind).toBe("OFF");
  });

  it("sees a flip made with the CLI through the stream", async () => {
    await until("the stream to open", () => used("/sdk/v1/stream") === 1);
    const before = used("/sdk/v1/ruleset");

    const on = await cli.run(["flags", "on", "new-checkout", "--project", PROJECT, "--env", ENV]);
    expect(on.code).toBe(0);
    await until(
      "the SDK to serve true",
      () => sdk().variation("new-checkout", { key: "someone" }, FALLBACK) === true,
    );

    const off = await cli.run(["flags", "off", "new-checkout", "--project", PROJECT, "--env", ENV]);
    expect(off.code).toBe(0);
    await until(
      "the SDK to serve false",
      () => sdk().variation("new-checkout", { key: "someone" }, FALLBACK) === false,
    );

    // Two changes, two refetches, one stream connection, and no poll (the interval is ten minutes).
    expect(used("/sdk/v1/ruleset")).toBe(before + 2);
    expect(used("/sdk/v1/stream")).toBe(1);
    expect(sdkErrors).toEqual([]);
  });

  it("still answers what the flags were under the hash from before a flip", async () => {
    const earlier = lastTag();

    const on = await cli.run(["flags", "on", "new-checkout", "--project", PROJECT, "--env", ENV]);
    expect(on.code).toBe(0);
    await until(
      "the SDK to serve true",
      () => sdk().variation("new-checkout", { key: "someone" }, FALLBACK) === true,
    );
    const later = lastTag();
    expect(later).not.toBe(earlier);

    const past = record(await cli.json(show(earlier)));
    expect(past["etag"]).toBe(earlier);
    expect(flagIn(past)["on"]).toBe(false);
    expect(flagIn(await cli.json(show(later)))["on"]).toBe(true);

    const off = await cli.run(["flags", "off", "new-checkout", "--project", PROJECT, "--env", ENV]);
    expect(off.code).toBe(0);
    await until(
      "the SDK to serve false again",
      () => sdk().variation("new-checkout", { key: "someone" }, FALLBACK) === false,
    );
  });

  it("agrees between POST /sdk/v1/evaluate and the SDK's local evaluation", async () => {
    const flagsBase = `/api/v1/projects/${PROJECT}/flags`;
    const configPath = (flag: string) => `${flagsBase}/${flag}/environments/${ENV}`;
    const create = (key: string, variations: unknown[]) =>
      cli.json([
        "flags",
        "create",
        key,
        "--project",
        PROJECT,
        "--variations",
        JSON.stringify(variations),
      ]);

    async function configure(flag: string, config: Record<string, unknown>): Promise<void> {
      const current = record(await admin.get(configPath(flag)));
      await admin.send("PUT", configPath(flag), {
        on: true,
        targets: [],
        prerequisites: [],
        rules: [],
        ...config,
        version: current["version"],
      });
    }

    for (const flag of FLAGS) await create(flag.key, flag.variations);
    await admin.send("POST", `/api/v1/projects/${PROJECT}/environments/${ENV}/segments`, SEGMENT);
    // In order, with `ready-marker` last: the SDK having it means it has every change above it (a ruleset is one snapshot).
    for (const flag of FLAGS) await configure(flag.key, flag.config);
    await until(
      "the SDK to serve the last change",
      () => sdk().variation("ready-marker", { key: "someone" }, FALLBACK) === true,
    );

    let compared = 0;
    const served = new Set<string>();
    for (const context of contexts()) {
      const response = await fetch(`${server?.url}/sdk/v1/evaluate`, {
        method: "POST",
        headers: { Authorization: `Bearer ${clientKey}`, "Content-Type": "application/json" },
        body: JSON.stringify({ context }),
      });
      expect(response.status).toBe(200);
      const flags = record(record(await response.json())["flags"]);
      expect(Object.keys(flags)).toContain("plan-gate");

      for (const [flag, raw] of Object.entries(flags)) {
        const remote = record(raw);
        const local = sdk().variationDetail<unknown>(flag, context, FALLBACK);
        // Nothing served is a null from the server and the caller's fallback from the SDK.
        const value = local.variationIndex === null ? remote["value"] : local.value;
        expect({ flag, context, index: local.variationIndex, reason: local.reason, value }).toEqual(
          {
            flag,
            context,
            index: remote["variationIndex"],
            reason: remote["reason"],
            value: remote["value"],
          },
        );
        served.add(`${flag}:${local.variationIndex}:${local.reason.kind}`);
        compared += 1;
      }
    }
    // Every flag is compared for every context, and the set reaches more than one branch of each rule.
    expect(compared).toBe(contexts().length * 8);
    for (const reason of [
      "TARGET_MATCH",
      "RULE_MATCH",
      "FALLTHROUGH",
      "OFF",
      "PREREQUISITE_FAILED",
    ]) {
      expect([...served].some((s) => s.endsWith(`:${reason}`))).toBe(true);
    }
    expect([...served].filter((s) => s.startsWith("rollout-split:")).length).toBeGreaterThan(2);
  });

  it("keeps a client key off the ruleset", async () => {
    const response = await fetch(`${server?.url}/sdk/v1/ruleset`, {
      headers: { Authorization: `Bearer ${clientKey}` },
    });
    expect(response.status).toBe(403);
  });

  it("counts what the SDK served, once it closes", async () => {
    await sdk().close();
    const usage = record(await admin.get(`/api/v1/projects/${PROJECT}/flags/new-checkout/usage`));
    const environments = usage["environments"];
    expect(Array.isArray(environments) ? environments.length : 0).toBe(1);
    expect(
      Number(record(Array.isArray(environments) ? environments[0] : null)["total"]),
    ).toBeGreaterThan(0);
  });
});
