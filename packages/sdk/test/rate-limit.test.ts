import { describe, expect, it } from "vitest";
import { useHarness, user } from "./harness.ts";

const h = useHarness();

const RULESET = "/sdk/v1/ruleset";
const STREAM = "/sdk/v1/stream";
const EVENTS = "/sdk/v1/events";
const FIVE_MINUTES = 300_000;

const INVALID_RETRY_AFTER = [null, "", "soon", "-1", "1.5", "Wed, 21 Oct 2026 07:28:00 GMT"];

/** How many requests a route has had. */
function hits(path: string): number {
  return h.server.requestsTo(path).length;
}

/** A client whose first request to `path` will have happened and been answered 429 once this returns. */
async function limitedOn(path: string, retryAfter: string | null): Promise<void> {
  h.server.limited.set(path, retryAfter);
  if (path === RULESET) {
    h.start({ stream: false, pollIntervalMs: 1_000 });
    await h.clock.advance(0);
    return;
  }
  if (path === STREAM) {
    h.start();
    await h.clock.advance(0);
    return;
  }
  const client = h.start({ stream: false, pollIntervalMs: 1_000_000, flushIntervalMs: 1_000 });
  await client.ready();
  client.variation("checkout", user, false);
  await h.clock.advance(1_000);
}

describe("a 429 on any route", () => {
  it.each([RULESET, STREAM, EVENTS])(
    "%s waits out Retry-After to the millisecond",
    async (path) => {
      await limitedOn(path, "120");
      h.server.limited.delete(path);
      expect(hits(path)).toBe(1);

      await h.clock.advance(119_999);
      expect(hits(path)).toBe(1);

      await h.clock.advance(1);
      expect(hits(path)).toBe(2);
    },
  );

  it.each([RULESET, STREAM, EVENTS])("%s caps a long Retry-After at 5 minutes", async (path) => {
    await limitedOn(path, "86400");
    h.server.limited.delete(path);

    await h.clock.advance(FIVE_MINUTES - 1);
    expect(hits(path)).toBe(1);

    await h.clock.advance(1);
    expect(hits(path)).toBe(2);
  });

  it.each(INVALID_RETRY_AFTER)(
    "ruleset falls back to polling with Retry-After %j",
    async (value) => {
      await limitedOn(RULESET, value);

      await h.clock.advance(1_000);

      expect(hits(RULESET)).toBe(2);
    },
  );

  it("stream falls back to its backoff with no Retry-After", async () => {
    await limitedOn(STREAM, null);

    await h.clock.advance(1_999);
    expect(hits(STREAM)).toBe(1);

    await h.clock.advance(1);
    expect(hits(STREAM)).toBe(2);
  });
});

describe("the ruleset", () => {
  it("does not go straight to the stream after a 429 on the first load", async () => {
    h.server.limited.set(RULESET, "60");
    h.start();

    await h.clock.advance(59_999);
    expect(hits(RULESET)).toBe(1);
    expect(hits(STREAM)).toBe(0);

    await h.clock.advance(1);
    expect(hits(STREAM)).toBe(1);
  });

  it("holds the fetch a stream asked for when it answers 429, and the stream reconnect with it", async () => {
    const client = h.start();
    await client.ready();
    await h.clock.advance(0);
    h.server.limited.set(RULESET, "60");
    h.server.setFlags([]);
    h.server.publish();
    await h.clock.advance(0);
    const ruleset = hits(RULESET);
    const stream = hits(STREAM);

    await h.clock.advance(59_999);
    expect(hits(RULESET)).toBe(ruleset);
    expect(hits(STREAM)).toBe(stream);
    h.server.limited.delete(RULESET);

    await h.clock.advance(1);
    expect(hits(RULESET)).toBe(ruleset + 1);
    expect(client.variation("checkout", user, "fallback")).toBe("fallback");
  });

  it("keeps answering from the last ruleset while every route is limited", async () => {
    const client = h.start();
    await client.ready();
    await h.clock.advance(0);
    for (const path of [RULESET, STREAM, EVENTS]) h.server.limited.set(path, "60");
    h.server.dropStreams();

    await h.clock.advance(600_000);

    expect(client.variation("checkout", user, false)).toBe(true);
    expect(client.variation("legacy", user, true)).toBe(false);
    expect(h.errors.length).toBeGreaterThan(0);
  });

  it("answers the fallback, without throwing, when the first load was limited", async () => {
    h.server.limited.set(RULESET, "60");
    h.server.limited.set(STREAM, "60");
    const client = h.start();

    await expect(client.ready()).resolves.toBe(false);

    expect(client.variation("checkout", user, false)).toBe(false);
    await expect(client.close()).resolves.toBeUndefined();
  });

  it("closes at once during a wait", async () => {
    h.server.limited.set(STREAM, "300");
    const client = h.start();
    await h.clock.advance(0);

    await expect(client.close()).resolves.toBeUndefined();

    expect(hits(STREAM)).toBe(1);
  });
});

describe("usage counts", () => {
  const options = { stream: false, pollIntervalMs: 1_000_000, flushIntervalMs: 10_000 };

  it("survive a 429 and go out, summed, once it has passed", async () => {
    h.server.limited.set(EVENTS, "60");
    const client = h.start(options);
    await client.ready();
    client.variation("checkout", user, false);
    await h.clock.advance(10_000);
    expect(hits(EVENTS)).toBe(1);

    client.variation("checkout", user, false);
    h.server.limited.delete(EVENTS);
    await h.clock.advance(59_999);
    expect(hits(EVENTS)).toBe(1);

    await h.clock.advance(1);
    expect(hits(EVENTS)).toBe(2);
    expect(h.server.batches[1]).toEqual([
      { flag: "checkout", variationIndex: 1, count: 2, day: "2026-11-05" },
    ]);
  });

  it("survive a 429 with no Retry-After", async () => {
    h.server.limited.set(EVENTS, null);
    const client = h.start(options);
    await client.ready();
    client.variation("checkout", user, false);
    await h.clock.advance(10_000);

    h.server.limited.delete(EVENTS);
    await h.clock.advance(10_000);

    expect(h.server.batches[1]).toEqual([
      { flag: "checkout", variationIndex: 1, count: 1, day: "2026-11-05" },
    ]);
  });

  it("are not sent by close during a wait, which says they were lost and does not throw", async () => {
    h.server.limited.set(EVENTS, "60");
    const client = h.start(options);
    await client.ready();
    client.variation("checkout", user, false);
    await h.clock.advance(10_000);
    const before = h.errors.length;

    await expect(client.close()).resolves.toBeUndefined();

    expect(hits(EVENTS)).toBe(1);
    expect(h.errors).toHaveLength(before + 1);
    expect(String(h.errors.at(-1))).toContain("1 usage counts unsent");
  });

  it("are sent by close once the wait has passed", async () => {
    h.server.limited.set(EVENTS, "5");
    const client = h.start(options);
    await client.ready();
    client.variation("checkout", user, false);
    await h.clock.advance(10_000);
    h.server.limited.delete(EVENTS);
    await h.clock.advance(5_000);

    await client.close();

    expect(hits(EVENTS)).toBe(2);
  });
});
