import { describe, expect, it } from "vitest";
import type { EvaluationEvent } from "../src/index.ts";
import { settle } from "./fake-clock.ts";
import { flag } from "./fake-server.ts";
import { useHarness, user } from "./harness.ts";

const h = useHarness();

function recorder(): {
  events: EvaluationEvent[];
  hooks: { afterEvaluation: (e: EvaluationEvent) => void };
} {
  const events: EvaluationEvent[] = [];
  return { events, hooks: { afterEvaluation: (event) => void events.push(event) } };
}

describe("hooks.afterEvaluation", () => {
  it("gets the key, context, variation, value, reason, ETag held and flag version", async () => {
    const seen = recorder();
    const client = h.start({ hooks: seen.hooks });
    await client.ready();

    client.variation("checkout", user, false);

    expect(seen.events).toEqual([
      {
        flagKey: "checkout",
        context: user,
        value: true,
        variationIndex: 1,
        reason: { kind: "FALLTHROUGH" },
        etag: h.server.etag,
        flagVersion: 1,
      },
    ]);
    expect(seen.events[0]?.etag).toBe(client.status().etag);
  });

  it("is told the ETag the evaluation used, not a later one", async () => {
    const seen = recorder();
    const client = h.start({ hooks: seen.hooks });
    await client.ready();
    await settle();

    client.variation("checkout", user, false);
    const before = client.status().etag;
    h.server.setFlags([flag("checkout", false)]);
    h.server.publish();
    await settle();
    client.variation("checkout", user, true);

    expect(seen.events.map((e) => e.etag)).toEqual([before, client.status().etag]);
    expect(before).not.toBe(client.status().etag);
  });

  it("is called for a fallback too, with a null index and a null ETag before the first load", () => {
    const seen = recorder();
    const client = h.start({ hooks: seen.hooks });

    expect(client.variation("checkout", user, "fb")).toBe("fb");

    expect(seen.events).toEqual([
      {
        flagKey: "checkout",
        context: user,
        value: "fb",
        variationIndex: null,
        reason: { kind: "ERROR", code: "NOT_READY" },
        etag: null,
        flagVersion: null,
      },
    ]);
  });

  it("reports an unknown flag and a type mismatch with a null flag version only for the first", async () => {
    const seen = recorder();
    const client = h.start({ hooks: seen.hooks });
    await client.ready();

    client.variation("missing", user, true);
    client.variation("checkout", user, "a string");

    expect(seen.events.map((e) => [e.reason, e.flagVersion, e.value])).toEqual([
      [{ kind: "ERROR", code: "FLAG_NOT_FOUND" }, null, true],
      [{ kind: "ERROR", code: "TYPE_MISMATCH" }, 1, "a string"],
    ]);
  });

  it("sends a throwing hook to onError and changes nothing for the caller", async () => {
    const boom = new Error("hook failed");
    const client = h.start({
      hooks: {
        afterEvaluation: () => {
          throw boom;
        },
      },
    });
    await client.ready();

    expect(client.variationDetail("checkout", user, false)).toEqual({
      value: true,
      variationIndex: 1,
      reason: { kind: "FALLTHROUGH" },
    });
    expect(h.errors).toEqual([boom]);
    await client.close();
    // The evaluation was still counted.
    expect(h.server.batches).toEqual([[expect.objectContaining({ flag: "checkout", count: 1 })]]);
  });

  it("sends a rejected promise to onError, and a hook that also throws from onError is ignored", async () => {
    const boom = new Error("async failure");
    const client = h.start({
      hooks: {
        afterEvaluation: async () => {
          throw boom;
        },
      },
      onError: (error) => {
        h.errors.push(error);
        throw new Error("onError threw");
      },
    });
    await client.ready();

    expect(client.variation("checkout", user, false)).toBe(true);
    await settle();
    expect(h.errors).toEqual([boom]);
  });
});
