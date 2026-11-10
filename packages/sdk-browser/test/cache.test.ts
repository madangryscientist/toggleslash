import { describe, expect, test } from "vitest";
import { settle } from "./fake-clock.ts";
import { served } from "./fake-server.ts";
import { start, started } from "./harness.ts";

describe("the cache", () => {
  test("serves the answer after ready, and the fallback with NOT_READY before", async () => {
    const { client } = start({ checkout: served(true, 1) });
    expect(client.variation("checkout", false)).toBe(false);
    expect(client.detail("checkout", false)).toEqual({
      value: false,
      variationIndex: null,
      reason: { kind: "ERROR", code: "NOT_READY" },
    });
    await client.ready();
    expect(client.variation("checkout", false)).toBe(true);
    expect(client.allFlags()).toEqual({ checkout: true });
    await client.close();
  });

  test("serves a bootstrap value before the first answer, as not ready, without an index", async () => {
    const { client, server } = start(
      { checkout: served(true, 1) },
      { bootstrap: { checkout: true, name: "x" } },
    );
    expect(client.variation("checkout", false)).toBe(true);
    expect(client.detail("checkout", false)).toEqual({
      value: true,
      variationIndex: null,
      reason: { kind: "ERROR", code: "NOT_READY" },
    });
    expect(client.allFlags()).toEqual({ checkout: true, name: "x" });
    // a bootstrap value of the wrong type is a mismatch, not a value
    expect(client.detail("name", 0).reason).toEqual({ kind: "ERROR", code: "TYPE_MISMATCH" });
    await client.ready();
    client.variation("checkout", false); // a cache hit now
    // the answer replaces it whole: a flag it does not carry is now not found
    expect(client.detail("name", "d")).toMatchObject({
      value: "d",
      reason: { code: "FLAG_NOT_FOUND" },
    });
    await client.close();
    expect(server.batches.flat().map((entry) => entry["flag"])).toEqual(["checkout"]);
  });

  test("maps the flat reason into the union and drops the null fields", async () => {
    const reasons = {
      a: { kind: "OFF", ruleIndex: null, flagKey: null, code: null },
      b: { kind: "TARGET_MATCH", ruleIndex: null, flagKey: null, code: null },
      c: { kind: "RULE_MATCH", ruleIndex: 2, flagKey: null, code: null },
      d: { kind: "FALLTHROUGH", ruleIndex: null, flagKey: null, code: null },
      e: { kind: "PREREQUISITE_FAILED", ruleIndex: null, flagKey: "base", code: null },
      f: { kind: "ERROR", ruleIndex: null, flagKey: null, code: "MALFORMED_FLAG" },
      g: { kind: "SOMETHING_NEW", ruleIndex: null, flagKey: null, code: null },
    };
    const answer = Object.fromEntries(
      Object.entries(reasons).map(([key, reason]) => [
        key,
        { value: 1, variationIndex: 0, reason },
      ]),
    );
    const { client } = await started(answer);
    const read = (key: string): unknown => client.detail(key, 0).reason;
    expect(read("a")).toStrictEqual({ kind: "OFF" });
    expect(read("b")).toStrictEqual({ kind: "TARGET_MATCH" });
    expect(read("c")).toStrictEqual({ kind: "RULE_MATCH", ruleIndex: 2 });
    expect(read("d")).toStrictEqual({ kind: "FALLTHROUGH" });
    expect(read("e")).toStrictEqual({ kind: "PREREQUISITE_FAILED", flagKey: "base" });
    expect(read("f")).toStrictEqual({ kind: "ERROR", code: "MALFORMED_FLAG" });
    expect(read("g")).toStrictEqual({ kind: "ERROR", code: "EVALUATION_FAILED" });
    await client.close();
  });

  test("a missing flag is FLAG_NOT_FOUND after an answer", async () => {
    const { client } = await started({ checkout: served(true, 1) });
    expect(client.detail("nope", "d")).toEqual({
      value: "d",
      variationIndex: null,
      reason: { kind: "ERROR", code: "FLAG_NOT_FOUND" },
    });
    await client.close();
  });

  test("a value of another type than the fallback is TYPE_MISMATCH; a null fallback takes any", async () => {
    const { client } = await started({
      n: served(5, 1),
      list: served([1], 0),
      nothing: served(null, 0),
    });
    expect(client.detail("n", "x")).toEqual({
      value: "x",
      variationIndex: null,
      reason: { kind: "ERROR", code: "TYPE_MISMATCH" },
    });
    expect(client.variation("list", {})).toEqual({});
    expect(client.variation<unknown>("n", null)).toBe(5);
    expect(client.variation("nothing", "x")).toBe("x");
    await client.close();
  });

  test("a flag that serves nothing gives the fallback with the server's reason", async () => {
    const reason = { kind: "ERROR", ruleIndex: null, flagKey: null, code: "FLAG_NOT_FOUND" };
    const { client } = await started({ gone: { value: null, variationIndex: null, reason } });
    expect(client.detail("gone", 7)).toEqual({
      value: 7,
      variationIndex: null,
      reason: { kind: "ERROR", code: "FLAG_NOT_FOUND" },
    });
    await client.close();
  });

  test("a failed first evaluate still settles ready, and flags stay NOT_READY", async () => {
    const { client, errors } = start({ checkout: served(true, 1) }, {}, (server) => {
      server.statuses.set("/sdk/v1/evaluate", { status: 500 });
    });
    await client.ready();
    expect(errors).toHaveLength(1);
    expect(client.detail("checkout", false).reason).toEqual({ kind: "ERROR", code: "NOT_READY" });
    await client.close();
  });

  test("a malformed answer keeps the last good values and reports", async () => {
    const { client, server, errors } = await started({ checkout: served(true, 1) });
    server.fallback = { checkout: { value: true } };
    server.publish();
    await settle();
    expect(errors).toHaveLength(1);
    expect(client.variation("checkout", false)).toBe(true);
    await client.close();
  });

  test("no method throws or rejects when fetch rejects", async () => {
    const { client, errors } = start({ checkout: served(true, 1) }, {}, (server) => {
      server.down = true;
    });
    await expect(client.ready()).resolves.toBeUndefined();
    expect(client.variation("checkout", false)).toBe(false);
    await expect(client.identify({ key: "bob" })).resolves.toBeUndefined();
    expect(errors.length).toBeGreaterThan(0);
    await expect(client.close()).resolves.toBeUndefined();
    await expect(client.close()).resolves.toBeUndefined();
  });

  test("an onError that throws, and a context that cannot be serialised, do not throw", async () => {
    const circular: Record<string, unknown> = {};
    circular["self"] = circular;
    const { client } = start(
      {},
      {
        context: { key: "a", attributes: circular },
        onError: () => {
          throw new Error("boom");
        },
      },
    );
    await expect(client.ready()).resolves.toBeUndefined();
    await client.close();
  });
});
