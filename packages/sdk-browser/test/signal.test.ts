import { describe, expect, test } from "vitest";
import { settle } from "./fake-clock.ts";
import { matched, served } from "./fake-server.ts";
import { start, started, URL_BASE } from "./harness.ts";

const EVALUATE = "/sdk/v1/evaluate";
const CHANGES = "/sdk/v1/changes";

describe("the request", () => {
  test("carries the key in a header and never in a URL; the context only in the evaluate body", async () => {
    const { client, server } = await started(
      { a: served(true, 1) },
      { context: { key: "alice", attributes: { plan: "pro" } } },
    );
    await client.close();
    expect(server.seen.length).toBeGreaterThan(1);
    for (const seen of server.seen) {
      expect(seen.headers.get("Authorization")).toBe(`Bearer ${server.key}`);
      expect(seen.url).not.toContain(server.key);
      expect(seen.url).not.toContain("alice");
      expect(new URL(seen.url).search).toBe("");
    }
    expect(server.requestsTo(EVALUATE)[0]?.body).toEqual({
      context: { key: "alice", attributes: { plan: "pro" } },
    });
    expect(server.requestsTo(CHANGES)[0]?.method).toBe("GET");
    expect(server.requestsTo(CHANGES)[0]?.body).toBeUndefined();
    expect(server.requestsTo(EVALUATE)[0]?.url).toBe(`${URL_BASE}${EVALUATE}`);
  });

  test("a trailing slash on the url is dropped", async () => {
    const { client, server } = await started({}, { url: `${URL_BASE}///` });
    await client.close();
    expect(server.seen[0]?.url).toBe(`${URL_BASE}${EVALUATE}`);
  });
});

describe("the change signal", () => {
  test("connected and changed each evaluate again; change fires with exactly the changed keys, sorted", async () => {
    const { client, server } = await started({
      b: served(1, 0),
      a: served("x", 0),
      c: served(true, 1),
    });
    const baseline = server.requestsTo(EVALUATE).length;
    expect(baseline).toBe(2); // the first answer, then connected
    const seen: string[][] = [];
    client.on("change", (flags) => seen.push(flags));

    server.fallback = { b: served(2, 1), a: served("x", 0), d: served(1, 0) }; // b changed, c gone, d new
    server.publish();
    await settle();
    expect(server.requestsTo(EVALUATE)).toHaveLength(baseline + 1);
    expect(seen).toEqual([["b", "c", "d"]]);
    expect(client.variation("b", 0)).toBe(2);

    server.publish(); // nothing changed
    await settle();
    expect(server.requestsTo(EVALUATE)).toHaveLength(baseline + 2);
    expect(seen).toHaveLength(1);
    await client.close();
  });

  test("a reason that changes with the same value and index fires change", async () => {
    const { client, server } = await started({ a: served(true, 0) });
    const seen: string[][] = [];
    client.on("change", (flags) => seen.push(flags));

    server.fallback = { a: matched(true, 0) };
    server.publish();
    await settle();
    expect(seen).toEqual([["a"]]);
    expect(client.detail("a", false).reason).toStrictEqual({ kind: "RULE_MATCH", ruleIndex: 0 });
    await client.close();
  });

  test("a rule moving under the same variation fires change", async () => {
    const { client, server } = await started({ a: matched(true, 0) });
    const seen: string[][] = [];
    client.on("change", (flags) => seen.push(flags));

    server.fallback = { a: matched(true, 1) };
    server.publish();
    await settle();
    expect(seen).toEqual([["a"]]);
    expect(client.detail("a", false).reason).toStrictEqual({ kind: "RULE_MATCH", ruleIndex: 1 });
    await client.close();
  });

  test("an identical answer fires nothing", async () => {
    const { client, server } = await started({ a: matched(true, 2, 1), b: served("x", 0) });
    const baseline = server.requestsTo(EVALUATE).length;
    const seen: string[][] = [];
    client.on("change", (flags) => seen.push(flags));

    server.publish();
    await settle();
    expect(server.requestsTo(EVALUATE)).toHaveLength(baseline + 1);
    expect(seen).toEqual([]);
    await client.close();
  });

  test("the first answer reports every key as appeared; an unsubscribed listener hears nothing", async () => {
    const { client } = start({ a: served(1, 0), b: served(1, 0) });
    const heard: string[][] = [];
    const quiet: string[][] = [];
    client.on("change", (flags) => heard.push(flags));
    client.on("change", (flags) => quiet.push(flags))();
    await client.ready();
    expect(heard).toEqual([["a", "b"]]);
    expect(quiet).toEqual([]);
    await client.close();
  });

  test("a throwing listener goes to onError and the others still run", async () => {
    const { client, server, errors } = start({ a: served(1, 0) });
    const heard: string[][] = [];
    client.on("change", () => {
      throw new Error("listener");
    });
    client.on("change", (flags) => heard.push(flags));
    await client.ready();
    await settle();
    expect(heard).toEqual([["a"]]);
    expect(errors.map((error) => (error instanceof Error ? error.message : error))).toEqual([
      "listener",
    ]);
    expect(server.requestsTo(EVALUATE).length).toBeGreaterThan(0);
    await client.close();
  });

  test("two signals during one evaluate cause one more evaluate, not two", async () => {
    const { client, server } = await started({ a: served(1, 0) });
    const baseline = server.requestsTo(EVALUATE).length;
    const release = server.hold();
    server.publish();
    await settle();
    expect(server.requestsTo(EVALUATE)).toHaveLength(baseline + 1);
    server.publish();
    server.publish();
    await settle();
    expect(server.requestsTo(EVALUATE)).toHaveLength(baseline + 1);
    release();
    await settle();
    expect(server.requestsTo(EVALUATE)).toHaveLength(baseline + 2);
    await client.close();
  });

  test("reconnect waits double from 1 s to 30 s and reset on connected", async () => {
    const { client, server, clock } = start({}, { requestTimeoutMs: 1_000_000 }, (fake) => {
      fake.statuses.set(CHANGES, { status: 500 });
    });
    await clock.advance(200_000);
    const gaps = (): number[] => {
      const times = server.requestsTo(CHANGES).map((seen) => seen.at);
      return times.slice(1).map((at, index) => at - (times[index] ?? 0));
    };
    expect(gaps().slice(0, 7)).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000]);

    server.statuses.delete(CHANGES);
    await clock.advance(30_000);
    expect(server.openStreams).toBe(1);
    const droppedAt = clock.now();
    server.dropStreams();
    server.statuses.set(CHANGES, { status: 500 });
    await clock.advance(10_000);
    // reset by the connected event: 1 s after the drop, then 2 s, then 4 s
    const retries = server.requestsTo(CHANGES).filter((seen) => seen.at > droppedAt);
    expect(retries.map((seen) => seen.at - droppedAt)).toEqual([1000, 3000, 7000]);
    await client.close();
  });

  test("a 429 waits its Retry-After", async () => {
    const { client, server, clock } = start({}, {}, (fake) => {
      fake.statuses.set(CHANGES, { status: 429, retryAfter: "7" });
    });
    await clock.advance(7_500);
    expect(server.requestsTo(CHANGES)).toHaveLength(2);
    const [first, second] = server.requestsTo(CHANGES);
    expect((second?.at ?? 0) - (first?.at ?? 0)).toBe(7000);
    await client.close();
  });

  test("a stream that goes quiet is dropped and reopened", async () => {
    const { client, server, clock } = await started({});
    expect(server.requestsTo(CHANGES)).toHaveLength(1);
    await clock.advance(61_000);
    expect(server.requestsTo(CHANGES).length).toBeGreaterThan(1);
    await client.close();
  });
});

describe("polling", () => {
  test("runs only while the signal is down", async () => {
    const { client, server, clock } = await started(
      { a: served(1, 0) },
      { pollIntervalMs: 10_000 },
    );
    const baseline = server.requestsTo(EVALUATE).length;
    await clock.advance(35_000);
    expect(server.requestsTo(EVALUATE)).toHaveLength(baseline); // signal up: no polling

    server.statuses.set(CHANGES, { status: 500 });
    server.dropStreams();
    await clock.advance(35_000);
    const polled = server.requestsTo(EVALUATE).length - baseline;
    expect(polled).toBeGreaterThanOrEqual(3);

    server.statuses.delete(CHANGES);
    await clock.advance(31_000); // reconnects; connected evaluates once
    const settledAt = server.requestsTo(EVALUATE).length;
    await clock.advance(35_000);
    expect(server.requestsTo(EVALUATE)).toHaveLength(settledAt);
    await client.close();
  });

  test("a poll picks up a change the signal never announced", async () => {
    const { client, server, clock } = await started(
      { a: served(1, 0) },
      { pollIntervalMs: 10_000 },
    );
    server.statuses.set(CHANGES, { status: 500 });
    server.dropStreams();
    server.fallback = { a: served(2, 1) };
    await clock.advance(25_000);
    expect(client.variation("a", 0)).toBe(2);
    await client.close();
  });
});

describe("identify", () => {
  test("replaces the cache, serving the previous context's values until the answer arrives", async () => {
    const { client, server } = await started({ plan: served("free", 0) });
    server.answers.set("bob", { plan: served("pro", 1) });
    const heard: string[][] = [];
    client.on("change", (flags) => heard.push(flags));
    const release = server.hold();
    const done = client.identify({ key: "bob" });
    await settle();
    expect(client.variation("plan", "?")).toBe("free");
    release();
    await done;
    expect(client.variation("plan", "?")).toBe("pro");
    expect(heard).toEqual([["plan"]]);
    expect(server.requestsTo(EVALUATE).at(-1)?.body).toEqual({ context: { key: "bob" } });
    await client.close();
  });

  test("a slow old answer arriving after a new one is dropped", async () => {
    const { client, server } = await started({ plan: served("free", 0) });
    server.answers.set("bob", { plan: served("pro", 1) });
    const releaseOld = server.hold();
    server.fallback = { plan: served("changed-for-alice", 0) };
    server.publish(); // a changed signal: an evaluate for alice, held
    await settle();
    await client.identify({ key: "bob" }); // bob's answer arrives first
    expect(client.variation("plan", "?")).toBe("pro");
    releaseOld();
    await settle();
    expect(client.variation("plan", "?")).toBe("pro");
    await client.close();
  });
});

describe("a refused key", () => {
  test("a 401 stops the signal, the polling and usage, and reports once", async () => {
    const { client, server, errors, clock } = await started(
      { a: served(1, 0) },
      { pollIntervalMs: 5_000 },
    );
    client.variation("a", 0);
    server.statuses.set(EVALUATE, { status: 401 });
    server.publish();
    await settle();
    expect(errors).toHaveLength(1);
    const count = server.seen.length;
    await clock.advance(200_000);
    await client.close();
    expect(server.seen).toHaveLength(count);
    expect(server.batches).toEqual([]);
    expect(errors).toHaveLength(1);
    expect(client.variation("a", 0)).toBe(1); // the cache keeps serving
  });

  test("a 403 on the signal stops everything", async () => {
    const { client, server, errors, clock } = start(
      { a: served(1, 0) },
      { pollIntervalMs: 5_000 },
      (fake) => {
        fake.statuses.set(CHANGES, { status: 403 });
      },
    );
    await clock.advance(1_000);
    expect(errors).toHaveLength(1);
    const count = server.seen.length;
    await clock.advance(200_000);
    expect(server.seen).toHaveLength(count);
    await client.close();
    expect(errors).toHaveLength(1);
  });
});

describe("close", () => {
  test("stops requests and is safe twice", async () => {
    const { client, server, clock } = await started({ a: served(1, 0) });
    await client.close();
    const count = server.seen.length;
    await clock.advance(300_000);
    server.publish();
    await settle();
    expect(server.seen).toHaveLength(count);
    expect(server.openStreams).toBe(0);
    await client.close();
    await expect(client.identify({ key: "x" })).resolves.toBeUndefined();
  });
});
