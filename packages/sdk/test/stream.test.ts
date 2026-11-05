import { describe, expect, it } from "vitest";
import { settle } from "./fake-clock.ts";
import { flag } from "./fake-server.ts";
import { useHarness, user } from "./harness.ts";

const h = useHarness();

describe("the stream", () => {
  it("delivers a change without waiting for a poll", async () => {
    const client = h.start();
    await client.ready();
    await settle();

    h.server.setFlags([flag("checkout", false)]);
    h.server.publish();
    await settle();

    expect(client.variation("checkout", user, true)).toBe(false);
    expect(h.server.requestsTo("/sdk/v1/ruleset")).toHaveLength(2);
    expect(h.server.requestsTo("/sdk/v1/ruleset")[1]?.headers.get("If-None-Match")).toBe('"v1"');
  });

  it("ignores keep-alive comments and a tag it already holds", async () => {
    const client = h.start();
    await client.ready();
    await settle();
    h.server.write(": keep-alive\n\n");
    h.server.publish();
    await settle();
    expect(h.server.requestsTo("/sdk/v1/ruleset")).toHaveLength(1);
    expect(client.variation("checkout", user, false)).toBe(true);
  });

  it("reconnects after it drops, and catches up on what it missed", async () => {
    const client = h.start();
    await client.ready();
    await settle();
    expect(h.server.requestsTo("/sdk/v1/stream")).toHaveLength(1);

    h.server.dropStreams();
    h.server.setFlags([flag("checkout", false)]);
    await h.clock.advance(999);
    expect(h.server.requestsTo("/sdk/v1/stream")).toHaveLength(1);

    await h.clock.advance(1);
    expect(h.server.requestsTo("/sdk/v1/stream")).toHaveLength(2);
    expect(client.variation("checkout", user, true)).toBe(false);

    h.server.setFlags([flag("checkout", true)]);
    h.server.publish();
    await settle();
    expect(client.variation("checkout", user, false)).toBe(true);
  });

  it("backs off while the stream will not open, up to the poll interval, and polls meanwhile", async () => {
    h.server.streamStatus = 503;
    const client = h.start();
    await client.ready();
    const attempts = () => h.server.requestsTo("/sdk/v1/stream").length;
    await settle();
    expect(attempts()).toBe(1);

    await h.clock.advance(1_999);
    expect(attempts()).toBe(1);
    await h.clock.advance(1);
    expect(attempts()).toBe(2);
    await h.clock.advance(3_999);
    expect(attempts()).toBe(2);
    await h.clock.advance(1);
    expect(attempts()).toBe(3);

    await h.clock.advance(300_000);
    const before = attempts();
    await h.clock.advance(30_000);
    expect(attempts()).toBe(before + 1);
    expect(h.server.requestsTo("/sdk/v1/ruleset").length).toBeGreaterThan(5);
  });

  it("falls back to polling: a change reaches a client whose stream is down", async () => {
    h.server.streamStatus = 404;
    const client = h.start();
    await client.ready();
    h.server.setFlags([flag("checkout", false)]);
    await h.clock.advance(30_000);
    expect(client.variation("checkout", user, true)).toBe(false);
  });

  it("retries a refresh that failed after a change, instead of staying stale on a healthy stream", async () => {
    const client = h.start();
    await client.ready();
    await settle();

    h.server.setFlags([flag("checkout", false)]);
    h.server.rulesetBody = "<html>bad gateway</html>";
    h.server.publish();
    await settle();
    expect(client.variation("checkout", user, false)).toBe(true);
    expect(h.server.openStreams).toBe(0);

    h.server.rulesetBody = null;
    await h.clock.advance(2_000);
    expect(client.variation("checkout", user, true)).toBe(false);
  });

  it("loads on a healthy stream after a first load that failed", async () => {
    h.server.rulesetBody = "{}";
    const client = h.start();
    expect(await client.ready()).toBe(false);
    await settle();

    h.server.rulesetBody = null;
    await h.clock.advance(2_000);
    expect(client.variation("checkout", user, false)).toBe(true);
  });

  it("drops a stream that goes quiet and opens another", async () => {
    const client = h.start();
    await client.ready();
    await settle();
    await h.clock.advance(60_000);
    await h.clock.advance(1_000);
    expect(h.server.requestsTo("/sdk/v1/stream")).toHaveLength(2);
    expect(String(h.errors[0])).toContain("went quiet");
  });

  it("starts the backoff over after a stream that connected", async () => {
    h.server.streamStatus = 503;
    const client = h.start();
    await client.ready();
    await h.clock.advance(10_000);
    h.server.streamStatus = 200;
    await h.clock.advance(30_000);
    expect(h.server.openStreams).toBe(1);

    h.server.dropStreams();
    await h.clock.advance(1_000);
    expect(h.server.openStreams).toBe(1);
    expect(client.variation("checkout", user, false)).toBe(true);
  });
});

describe("a server that cannot be reached", () => {
  it("serves the caller's fallback when it was never reached, and loads once it is", async () => {
    h.server.down = true;
    const client = h.start();
    expect(await client.ready()).toBe(false);

    expect(client.variation("checkout", user, false)).toBe(false);
    expect(client.variationDetail("legacy", user, "fallback")).toEqual({
      value: "fallback",
      variationIndex: null,
      reason: { kind: "ERROR", code: "NOT_READY" },
    });
    expect(h.errors.length).toBeGreaterThan(0);

    h.server.down = false;
    await h.clock.advance(30_000);
    expect(client.variation("checkout", user, false)).toBe(true);
  });

  it("serves the last ruleset when it goes away later, and recovers", async () => {
    const client = h.start();
    await client.ready();
    await settle();

    await h.server.goDown();
    h.server.setFlags([flag("checkout", false)]);
    await h.clock.advance(120_000);
    expect(client.variation("checkout", user, false)).toBe(true);
    expect(h.errors.length).toBeGreaterThan(0);

    h.server.down = false;
    await h.clock.advance(30_000);
    expect(client.variation("checkout", user, true)).toBe(false);
  });

  it("does not hang ready() on a server that never answers", async () => {
    h.server.down = false;
    const client = h.start({
      fetch: (_input, init) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () => reject(init.signal?.reason), {
            once: true,
          });
        }),
    });
    const ready = client.ready();
    await h.clock.advance(10_000);
    expect(await ready).toBe(false);
    expect(client.variation("checkout", user, "kept")).toBe("kept");
  });

  it("keeps the last ruleset when the server sends something that is not one", async () => {
    const client = h.start({ stream: false });
    await client.ready();
    for (const body of ["<html>bad gateway</html>", "{}", "null", '{"flags":{}}']) {
      h.server.version++;
      h.server.rulesetBody = body;
      await h.clock.advance(30_000);
      expect(client.variation("checkout", user, false)).toBe(true);
    }
    expect(h.errors).toHaveLength(4);
  });

  it("keeps the last ruleset on a 401 or a 500", async () => {
    const client = h.start({ stream: false, sdkKey: "revoked" });
    expect(await client.ready()).toBe(false);
    expect(String(h.errors[0])).toContain("401");
  });

  it("absorbs a fetch that throws at once, and an onError that throws", async () => {
    const client = h.start({
      fetch: () => {
        throw new Error("no fetch for you");
      },
      onError: () => {
        throw new Error("a broken hook");
      },
    });
    expect(await client.ready()).toBe(false);
    await h.clock.advance(60_000);
    expect(client.variation("checkout", user, 7)).toBe(7);
    await client.close();
  });
});
