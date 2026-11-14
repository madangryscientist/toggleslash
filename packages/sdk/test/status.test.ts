import { describe, expect, it } from "vitest";
import { settle } from "./fake-clock.ts";
import { flag } from "./fake-server.ts";
import { useHarness } from "./harness.ts";

const h = useHarness();

/** A fetch that never answers, and gives up only when its signal aborts. */
function neverAnswers(_input: string, init?: RequestInit): Promise<Response> {
  return new Promise<Response>((_resolve, reject) => {
    init?.signal?.addEventListener("abort", () => reject(init.signal?.reason), { once: true });
  });
}

describe("status()", () => {
  it("reports nothing held before the first load", () => {
    const client = h.start();
    expect(client.status()).toEqual({
      etag: null,
      lastSyncAt: null,
      stream: "connecting",
      lastError: null,
    });
  });

  it("reports the ETag, the sync time and a connected stream after a load", async () => {
    const client = h.start();
    await client.ready();
    await settle();

    expect(client.status()).toEqual({
      etag: h.server.etag,
      lastSyncAt: h.clock.now(),
      stream: "connected",
      lastError: null,
    });
  });

  it("reports off without a stream, and a 304 moves the sync time and keeps the ETag", async () => {
    const client = h.start({ stream: false });
    await client.ready();
    const first = h.clock.now();

    await h.clock.advance(30_000);
    const poll = h.server.requestsTo("/sdk/v1/ruleset")[1];
    expect(poll?.headers.get("If-None-Match")).toBe(h.server.etag);
    expect(client.status()).toEqual({
      etag: h.server.etag,
      lastSyncAt: first + 30_000,
      stream: "off",
      lastError: null,
    });
  });

  it("moves the ETag and the sync time when a published change is loaded", async () => {
    const client = h.start();
    await client.ready();
    await settle();

    await h.clock.advance(5_000);
    h.server.setFlags([flag("checkout", false)]);
    h.server.publish();
    await settle();

    expect(client.status().etag).toBe('"v2"');
    expect(client.status().lastSyncAt).toBe(h.clock.now());
  });

  it("reports a disconnected stream when it drops, and connected again once it reconnects", async () => {
    const client = h.start();
    await client.ready();
    await settle();

    h.server.dropStreams();
    await settle();
    expect(client.status().stream).toBe("disconnected");

    await h.clock.advance(1_000);
    expect(client.status().stream).toBe("connected");
  });

  it("holds the ETag through a server that is down, reports a network error, and clears it on recovery", async () => {
    const client = h.start();
    await client.ready();
    await settle();

    await h.server.goDown();
    await h.clock.advance(1_000);
    expect(client.status()).toMatchObject({ etag: h.server.etag, lastError: "network" });

    h.server.down = false;
    await h.clock.advance(10_000);
    expect(client.status().lastError).toBeNull();
    expect(client.status().etag).toBe(h.server.etag);
  });

  it("reports the HTTP status of a refused ruleset", async () => {
    const client = h.start({ sdkKey: "tgl_server_wrong", stream: false });
    expect(await client.ready()).toBe(false);
    await settle();
    expect(client.status()).toMatchObject({ etag: null, lastError: "http_401" });
  });

  it("does not follow a redirect: the ruleset and the stream ask for manual", async () => {
    const client = h.start();
    await client.ready();
    await settle();
    for (const path of ["/sdk/v1/ruleset", "/sdk/v1/stream"]) {
      const requests = h.server.requestsTo(path);
      expect(requests.length).toBeGreaterThan(0);
      for (const seen of requests) expect(seen.redirect).toBe("manual");
    }
  });

  it("reports a redirected ruleset as its HTTP status", async () => {
    h.server.redirected.set("/sdk/v1/ruleset", 302);
    const client = h.start({ stream: false });
    expect(await client.ready()).toBe(false);
    await settle();
    expect(client.status()).toMatchObject({ etag: null, lastSyncAt: null, lastError: "http_302" });
    expect(h.errors).toHaveLength(1);
  });

  it("reports a bad ruleset when the body has no flags and segments", async () => {
    h.server.rulesetBody = "{}";
    const client = h.start({ stream: false });
    expect(await client.ready()).toBe(false);
    await settle();
    expect(client.status()).toMatchObject({ etag: null, lastError: "bad_ruleset" });
  });

  it("reports a timeout when the ruleset never answers", async () => {
    const client = h.start({ stream: false, requestTimeoutMs: 500, fetch: neverAnswers });

    await h.clock.advance(499);
    expect(client.status().lastError).toBeNull();
    await h.clock.advance(1);
    expect(client.status().lastError).toBe("timeout");
    expect(await client.ready()).toBe(false);
  });

  it("reports closed after close(), and still answers", async () => {
    const client = h.start();
    await client.ready();
    await settle();
    await client.close();

    expect(client.status()).toEqual({
      etag: h.server.etag,
      lastSyncAt: h.clock.now(),
      stream: "closed",
      lastError: null,
    });
  });

  it("makes no request", async () => {
    const client = h.start();
    await client.ready();
    await settle();

    const before = h.server.seen.length;
    client.status();
    client.status();
    expect(h.server.seen.length).toBe(before);
  });

  it("holds no SDK key", async () => {
    const client = h.start();
    await client.ready();
    await settle();

    expect(JSON.stringify(client.status())).not.toContain(h.server.key);
  });
});
