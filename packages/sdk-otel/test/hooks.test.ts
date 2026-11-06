import { type Client, createClient, type Fetch } from "@hackcove/toggleslash-sdk";
import { context, trace, type Span } from "@opentelemetry/api";
import { AsyncLocalStorageContextManager } from "@opentelemetry/context-async-hooks";
import {
  BasicTracerProvider,
  InMemorySpanExporter,
  SimpleSpanProcessor,
  type ReadableSpan,
} from "@opentelemetry/sdk-trace-base";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { createTracingHooks, type TracingOptions } from "../src/index.ts";

const exporter = new InMemorySpanExporter();
const provider = new BasicTracerProvider({ spanProcessors: [new SimpleSpanProcessor(exporter)] });
const tracer = provider.getTracer("test");
const manager = new AsyncLocalStorageContextManager();

beforeAll(() => {
  context.setGlobalContextManager(manager.enable());
  trace.setGlobalTracerProvider(provider);
});
afterAll(async () => {
  trace.disable();
  context.disable();
  await provider.shutdown();
});
beforeEach(() => exporter.reset());

const BASE: TracingOptions = { project: "shop", environment: "production" };
const HASH = "a".repeat(64);

function flag(key: string, overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    key,
    variations: [false, true],
    on: true,
    offVariation: 0,
    targets: [],
    prerequisites: [],
    rules: [],
    fallthrough: { variation: 1 },
    salt: "s",
    version: 7,
    ...overrides,
  };
}

/** The two SDK routes this test needs, as the server answers them: a strong quoted ETag, and 204 for events. */
function fakeFetch(flags: unknown[], etag = `"${HASH}"`): Fetch {
  return async (input) => {
    const path = new URL(input).pathname;
    if (path === "/sdk/v1/ruleset") {
      return new Response(JSON.stringify({ flags, segments: [] }), {
        headers: { ETag: etag, "Content-Type": "application/json" },
      });
    }
    return new Response(null, { status: 204 });
  };
}

const clients: Client[] = [];
afterEach(async () => {
  await Promise.all(clients.splice(0).map((client) => client.close()));
});

async function start(options: Partial<TracingOptions>, flags: unknown[] = [flag("checkout")]) {
  const errors: unknown[] = [];
  const client = createClient({
    url: "https://flags.example.test",
    sdkKey: "fs_server_test",
    fetch: fakeFetch(flags),
    stream: false,
    pollIntervalMs: 600_000,
    flushIntervalMs: 600_000,
    hooks: createTracingHooks({ ...BASE, ...options }),
    onError: (error) => errors.push(error),
  });
  clients.push(client);
  expect(await client.ready()).toBe(true);
  return { client, errors };
}

/** Runs `work` inside a span, ends it, and returns the finished span. */
function inSpan(work: () => void): ReadableSpan {
  tracer.startActiveSpan("request", (span: Span) => {
    try {
      work();
    } finally {
      span.end();
    }
  });
  const [finished] = exporter.getFinishedSpans();
  if (finished === undefined) throw new Error("no span was exported");
  return finished;
}

describe("the feature_flag.evaluation event", () => {
  it("has exactly the documented attributes by default", async () => {
    const { client } = await start({});
    const span = inSpan(() => client.variation("checkout", { key: "user-1" }, false));

    expect(span.events).toHaveLength(1);
    expect(span.events[0]?.name).toBe("feature_flag.evaluation");
    expect(span.events[0]?.attributes).toEqual({
      "feature_flag.key": "checkout",
      "feature_flag.provider.name": "ToggleSlash",
      "feature_flag.result.variant": "1",
      "feature_flag.result.reason": "fallthrough",
      "feature_flag.version": "7",
      "toggleslash.ruleset.etag": HASH,
      "toggleslash.project": "shop",
      "toggleslash.environment": "production",
    });
  });

  it("carries the ETag the client holds, lowercase and without quotes", async () => {
    const { client } = await start({});
    const span = inSpan(() => client.variation("checkout", { key: "user-1" }, false));

    const held = client.status().etag;
    expect(held).toBe(`"${HASH}"`);
    expect(span.events[0]?.attributes?.["toggleslash.ruleset.etag"]).toBe(
      held?.replaceAll('"', ""),
    );
  });

  it("leaves out the value and the context, even for a context with attributes", async () => {
    const { client } = await start({});
    const ctx = { key: "user-secret-42", attributes: { email: "a@b.example" } };
    const span = inSpan(() => client.variation("checkout", ctx, false));

    const attributes = span.events[0]?.attributes ?? {};
    expect(Object.keys(attributes)).not.toContain("feature_flag.context.id");
    expect(Object.keys(attributes)).not.toContain("feature_flag.result.value");
    expect(JSON.stringify(span.events)).not.toContain("user-secret-42");
    expect(JSON.stringify(span.events)).not.toContain("a@b.example");
  });

  it("adds the context id when asked, and only that", async () => {
    const { client } = await start({ includeContextId: true });
    const span = inSpan(() => client.variation("checkout", { key: "user-1" }, false));

    expect(span.events[0]?.attributes).toMatchObject({ "feature_flag.context.id": "user-1" });
    expect(Object.keys(span.events[0]?.attributes ?? {})).not.toContain(
      "feature_flag.result.value",
    );
  });

  it("adds the value when asked, and only that", async () => {
    const { client } = await start({ includeValue: true });
    const span = inSpan(() => client.variation("checkout", { key: "user-1" }, false));

    expect(span.events[0]?.attributes).toMatchObject({ "feature_flag.result.value": true });
    expect(Object.keys(span.events[0]?.attributes ?? {})).not.toContain("feature_flag.context.id");
  });

  it("writes a JSON value as its text", async () => {
    const flags = [
      flag("limits", { variations: [{ max: 3 }, null], fallthrough: { variation: 0 } }),
    ];
    const { client } = await start({ includeValue: true }, flags);
    const span = inSpan(() => client.variation("limits", { key: "u" }, null));

    expect(span.events[0]?.attributes?.["feature_flag.result.value"]).toBe('{"max":3}');
  });

  it("maps each reason, and names the error with error.type", async () => {
    const flags = [
      flag("off", { on: false }),
      flag("targeted", { targets: [{ variation: 1, contextKeys: ["vip"] }] }),
      flag("ruled", {
        rules: [{ clauses: [{ attribute: "plan", op: "in", values: ["pro"] }], variation: 1 }],
      }),
      flag("blocked", { prerequisites: [{ flagKey: "off", variation: 1 }] }),
    ];
    const { client } = await start({}, flags);
    const ctx = { key: "vip", attributes: { plan: "pro" } };
    const span = inSpan(() => {
      client.variation("off", ctx, null);
      client.variation("targeted", ctx, null);
      client.variation("ruled", { key: "x", attributes: { plan: "pro" } }, null);
      client.variation("blocked", ctx, null);
      client.variation("missing", ctx, null);
      client.variation("targeted", ctx, "a string");
    });

    expect(
      span.events.map((e) => [
        e.attributes?.["feature_flag.key"],
        e.attributes?.["feature_flag.result.reason"],
        e.attributes?.["feature_flag.result.variant"],
        e.attributes?.["error.type"],
      ]),
    ).toEqual([
      ["off", "disabled", "0", undefined],
      ["targeted", "targeting_match", "1", undefined],
      ["ruled", "targeting_match", "1", undefined],
      ["blocked", "prerequisite_failed", "0", undefined],
      ["missing", "error", undefined, "flag_not_found"],
      ["targeted", "error", undefined, "type_mismatch"],
    ]);
    expect(span.events[4]?.attributes).not.toHaveProperty("feature_flag.version");
  });

  it("is not an error with no active span, and records nothing", async () => {
    const { client, errors } = await start({});

    expect(trace.getActiveSpan()).toBeUndefined();
    expect(client.variation("checkout", { key: "user-1" }, false)).toBe(true);

    expect(errors).toEqual([]);
    expect(exporter.getFinishedSpans()).toEqual([]);
  });

  it("leaves the caller's answer unchanged when the span throws, and tells onError", async () => {
    const { client, errors } = await start({});
    const plain = createClient({
      url: "https://flags.example.test",
      sdkKey: "fs_server_test",
      fetch: fakeFetch([flag("checkout")]),
      stream: false,
    });
    clients.push(plain);
    await plain.ready();
    const boom = new Error("exporter exploded");
    const broken = tracer.startSpan("broken");
    broken.addEvent = () => {
      throw boom;
    };

    const ctx = { key: "user-1" };
    const answer = context.with(trace.setSpan(context.active(), broken), () =>
      client.variationDetail("checkout", ctx, false),
    );

    broken.end();

    expect(answer).toEqual(plain.variationDetail("checkout", ctx, false));
    expect(errors).toEqual([boom]);
  });

  it("refuses to be made without a project or an environment", () => {
    expect(() => createTracingHooks({ ...BASE, project: "" })).toThrow(TypeError);
    expect(() => createTracingHooks({ ...BASE, environment: "" })).toThrow(TypeError);
    expect(() => Reflect.apply(createTracingHooks, undefined, [{ environment: "x" }])).toThrow(
      TypeError,
    );
  });
});
