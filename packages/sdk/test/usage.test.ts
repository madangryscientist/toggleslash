import { describe, expect, it } from "vitest";
import { settle } from "./fake-clock.ts";
import { flag } from "./fake-server.ts";
import { useHarness, user } from "./harness.ts";

const h = useHarness();

describe("usage counts", () => {
  it("are batched, summed per flag, variation and day, and sent on close", async () => {
    const client = h.start();
    await client.ready();
    client.variation("checkout", user, false);
    client.variation("checkout", { key: "user-2" }, false);
    client.variation("checkout", { key: "user-3" }, false);
    client.variation("legacy", user, true);
    client.variation("missing", user, true);

    await client.close();

    expect(h.server.batches).toEqual([
      [
        { flag: "checkout", variationIndex: 1, count: 3, day: "2026-11-05" },
        { flag: "legacy", variationIndex: 0, count: 1, day: "2026-11-05" },
      ],
    ]);
    const request = h.server.requestsTo("/sdk/v1/events")[0];
    expect(request?.method).toBe("POST");
    expect(request?.headers.get("Authorization")).toBe(`Bearer ${h.server.key}`);
    expect(JSON.stringify(request?.body)).not.toContain("user-");
  });

  it("counts a flag the caller got the fallback for as nothing", async () => {
    const client = h.start();
    await client.ready();
    client.variation("checkout", user, "a string, not a boolean");
    client.variation("checkout", { key: "" }, false);
    await client.close();
    expect(h.server.requestsTo("/sdk/v1/events")).toHaveLength(0);
  });

  it("splits at the day boundary of the clock", async () => {
    const client = h.start();
    await client.ready();
    client.variation("checkout", user, false);
    h.clock.time = Date.UTC(2026, 10, 6, 0, 0, 1);
    client.variation("checkout", user, false);
    await client.close();
    expect(h.server.batches[0]?.map((event) => event["day"])).toEqual(["2026-11-05", "2026-11-06"]);
  });

  it("are sent every flush interval, not only on close", async () => {
    const client = h.start({ flushIntervalMs: 5_000 });
    await client.ready();
    client.variation("checkout", user, false);
    await h.clock.advance(4_999);
    expect(h.server.requestsTo("/sdk/v1/events")).toHaveLength(0);
    await h.clock.advance(1);
    expect(h.server.batches).toHaveLength(1);

    await h.clock.advance(5_000);
    expect(h.server.batches).toHaveLength(1);
  });

  it("are kept when the server is down and sent, summed, when it is back", async () => {
    const client = h.start({ flushIntervalMs: 5_000 });
    await client.ready();
    h.server.eventsStatus = 503;
    client.variation("checkout", user, false);
    await h.clock.advance(5_000);
    client.variation("checkout", user, false);
    h.server.eventsStatus = 200;
    await h.clock.advance(5_000);
    expect(h.server.batches).toHaveLength(2);
    expect(h.server.batches[1]).toEqual([
      { flag: "checkout", variationIndex: 1, count: 2, day: "2026-11-05" },
    ]);
  });

  it("are dropped when the server refuses the batch, since it would refuse it again", async () => {
    const client = h.start({ flushIntervalMs: 5_000 });
    await client.ready();
    h.server.eventsStatus = 400;
    client.variation("checkout", user, false);
    await h.clock.advance(5_000);
    await h.clock.advance(5_000);
    expect(h.server.batches).toHaveLength(1);
  });

  it("go in batches the server's limits allow", async () => {
    h.server.setFlags(Array.from({ length: 600 }, (_, index) => flag(`flag-${index}`, true)));
    const client = h.start();
    await client.ready();
    for (let index = 0; index < 600; index++) client.variation(`flag-${index}`, user, false);
    await client.close();
    expect(h.server.batches.map((batch) => batch.length)).toEqual([500, 100]);
  });

  it("do not make close() wait longer than the request timeout", async () => {
    const client = h.start();
    await client.ready();
    client.variation("checkout", user, false);
    h.server.eventsHang = true;
    let closed = false;
    const closing = client.close().then(() => {
      closed = true;
    });
    await h.clock.advance(9_999);
    expect(closed).toBe(false);
    await h.clock.advance(1);
    await closing;
    expect(closed).toBe(true);
  });
});

describe("close", () => {
  it("stops all requests, and is safe to call twice", async () => {
    const client = h.start();
    await client.ready();
    await settle();
    const first = client.close();
    expect(client.close()).toBe(first);
    await first;
    expect(h.server.openStreams).toBe(0);

    const count = h.server.seen.length;
    await h.clock.advance(300_000);
    expect(h.server.seen).toHaveLength(count);
  });

  it("still evaluates afterwards, from the last ruleset, and counts nothing", async () => {
    const client = h.start();
    await client.ready();
    await client.close();
    expect(client.variation("checkout", user, false)).toBe(true);
    await h.clock.advance(60_000);
    expect(h.server.requestsTo("/sdk/v1/events")).toHaveLength(0);
  });

  it("resolves ready() as false when closed before the first load", async () => {
    h.server.down = true;
    const client = h.start();
    await client.close();
    expect(await client.ready()).toBe(false);
  });
});
