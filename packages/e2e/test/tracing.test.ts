import { createClient, type Client } from "@hackcove/featureslash-sdk";
import { createTracingHooks } from "@hackcove/featureslash-sdk-otel";
import { context as otelContext, trace } from "@opentelemetry/api";
import { AsyncLocalStorageContextManager } from "@opentelemetry/context-async-hooks";
import {
  BasicTracerProvider,
  InMemorySpanExporter,
  SimpleSpanProcessor,
} from "@opentelemetry/sdk-trace-base";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { createAdmin } from "../src/admin.ts";
import { createCli, type Cli } from "../src/cli.ts";
import { record } from "../src/data.ts";
import { skipReason } from "../src/dotnet.ts";
import { seed, SEED_ENV, SEED_PROJECT } from "../src/seed.ts";
import { buildServer, startServer, type RunningServer } from "../src/server.ts";

/**
 * A real evaluation by the real SDK, against the real server, puts a hash on a span, and the lookup route
 * answers that hash with the variation the SDK served. Spans go to an in-memory exporter.
 */
const skip = skipReason() !== null;
const exporter = new InMemorySpanExporter();
const provider = new BasicTracerProvider({ spanProcessors: [new SimpleSpanProcessor(exporter)] });

const ADMIN_TOKEN = "e2e-tracing-throwaway-admin-token";
const FLAG = "new-checkout";
const PRO = { key: "pro-1", attributes: { plan: "pro" } };
const FREE = { key: "free-1", attributes: { plan: "free" } };

/** Both shoppers are served inside one span, so the span carries one event for each. */
function serveBoth(sdk: Client) {
  return provider.getTracer("e2e").startActiveSpan("checkout request", (span) => {
    try {
      return {
        pro: sdk.variationDetail(FLAG, PRO, null),
        free: sdk.variationDetail(FLAG, FREE, null),
      };
    } finally {
      span.end();
    }
  });
}

function attributesOfEvents(): [Attributes, Attributes] {
  const events = exporter.getFinishedSpans()[0]?.events ?? [];
  const [pro, free] = events.map((event) => event.attributes ?? {});
  if (pro === undefined || free === undefined)
    throw new Error("the span has fewer than two events");
  return [pro, free];
}

const rulesetOf = (answer: unknown): Record<string, unknown> => {
  const flags = record(answer)["flags"];
  return record(Array.isArray(flags) ? flags[0] : undefined);
};

type Attributes = Record<string, unknown>;

const showArgs = (hash: string, span: Attributes): string[] => [
  "rulesets",
  "show",
  hash,
  "--project",
  String(span["featureslash.project"]),
  "--env",
  String(span["featureslash.environment"]),
  "--flag",
  String(span["feature_flag.key"]),
];

describe.skipIf(skip)("a span's ruleset hash against the real server", () => {
  let scratch = "";
  let server: RunningServer | null = null;
  let client: Client | null = null;
  let cli: Cli;
  const sdkErrors: unknown[] = [];

  beforeAll(async () => {
    scratch = mkdtempSync(join(tmpdir(), "featureslash-tracing-e2e-"));
    mkdirSync(join(scratch, "home"));
    server = await startServer(await buildServer(), {
      dbPath: join(scratch, "featureslash.db"),
      adminToken: ADMIN_TOKEN,
    });
    cli = createCli({ url: server.url, token: ADMIN_TOKEN, home: join(scratch, "home") });
    const keys = await seed({ cli, admin: createAdmin({ url: server.url, token: ADMIN_TOKEN }) });
    otelContext.setGlobalContextManager(new AsyncLocalStorageContextManager().enable());
    client = createClient({
      url: server.url,
      sdkKey: keys.serverKey,
      stream: false,
      pollIntervalMs: 600_000,
      hooks: createTracingHooks({ project: SEED_PROJECT, environment: SEED_ENV }),
      onError: (error) => sdkErrors.push(error),
    });
    if (!(await client.ready())) throw new Error("the SDK never loaded the ruleset");
  });

  afterAll(async () => {
    try {
      await client?.close();
    } finally {
      try {
        await server?.stop();
      } finally {
        trace.disable();
        otelContext.disable();
        await provider.shutdown();
        rmSync(scratch, { recursive: true, force: true });
      }
    }
  });

  it("answers the hash on a span with the variation the SDK served", async () => {
    const sdk = client;
    if (sdk === null) throw new Error("the SDK client is not created yet");
    const served = serveBoth(sdk);
    const [pro, free] = attributesOfEvents();

    // The SDK served the rule's variation to one shopper and the fallthrough to the other.
    expect([served.pro.variationIndex, served.pro.reason.kind]).toEqual([0, "RULE_MATCH"]);
    expect([served.free.variationIndex, served.free.reason.kind]).toEqual([1, "FALLTHROUGH"]);
    expect(pro["feature_flag.result.variant"]).toBe("0");
    expect(free["feature_flag.result.variant"]).toBe("1");

    const hash = String(pro["featureslash.ruleset.etag"]);
    expect(hash).toMatch(/^[0-9a-f]{64}$/);
    expect(hash).toBe(sdk.status().etag?.replaceAll('"', ""));
    expect(free["featureslash.ruleset.etag"]).toBe(hash);

    // The lookup route knows that hash, and the flag under it serves what the span says.
    const lookup = await cli.json(showArgs(hash, pro));
    const flag = rulesetOf(lookup);
    const rules = Array.isArray(flag["rules"]) ? flag["rules"] : [];
    expect(record(lookup)["etag"]).toBe(`"${hash}"`);
    expect(record(rules[0])["variation"]).toBe(Number(pro["feature_flag.result.variant"]));
    expect(record(flag["fallthrough"])["variation"]).toBe(
      Number(free["feature_flag.result.variant"]),
    );
    expect(String(flag["version"])).toBe(pro["feature_flag.version"]);
    expect(sdkErrors).toEqual([]);
  });

  it("diagnoses at a past hash the answer the SDK served under it, after the flag has changed", async () => {
    const sdk = client;
    if (sdk === null) throw new Error("the SDK client is not created yet");
    const served = serveBoth(sdk);
    const hash = sdk.status().etag?.replaceAll('"', "") ?? "";
    expect(hash).toMatch(/^[0-9a-f]{64}$/);

    const diagnose = async (who: typeof PRO, extra: string[]) => {
      const answer = await cli.json([
        "diagnose",
        FLAG,
        "--project",
        SEED_PROJECT,
        "--env",
        SEED_ENV,
        "--context",
        JSON.stringify(who),
        ...extra,
      ]);
      const flags = record(record(answer)["flags"]);
      const { value, variationIndex, reason } = record(flags[FLAG]);
      return { value, variationIndex, reason };
    };
    const as = (detail: typeof served.pro) => ({
      value: detail.value,
      variationIndex: detail.variationIndex,
      reason: detail.reason,
    });

    await cli.json(["flags", "off", FLAG, "--project", SEED_PROJECT, "--env", SEED_ENV]);

    // With the hash: what the SDK served under it, for both shoppers, though the flag is now off.
    expect(await diagnose(PRO, ["--etag", hash])).toEqual(as(served.pro));
    expect(await diagnose(FREE, ["--etag", hash])).toEqual(as(served.free));
    // Without it: the flag as it is now.
    expect((await diagnose(PRO, [])).reason).toMatchObject({ kind: "OFF" });
    expect(sdkErrors).toEqual([]);
  });
});
