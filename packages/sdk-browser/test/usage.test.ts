import { describe, expect, test } from "vitest";
import { served } from "./fake-server.ts";
import { start, started } from "./harness.ts";

describe("usage", () => {
  test("records cache hits only, and sends them on close without any context", async () => {
    const { client, server } = await started({
      a: served(true, 1),
      n: served(5, 0),
      gone: {
        value: null,
        variationIndex: null,
        reason: { kind: "OFF", ruleIndex: null, flagKey: null, code: null },
      },
    });
    client.variation("a", false);
    client.detail("a", false);
    client.variation("missing", false); // not found: nothing
    client.variation("n", "x"); // type mismatch: nothing
    client.variation("gone", false); // serves nothing: nothing
    expect(server.batches).toEqual([]);
    await client.close();
    const entries = server.batches.flat();
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({ flag: "a", variationIndex: 1, count: 2 });
    const body = JSON.stringify(server.requestsTo("/sdk/v1/events")[0]?.body);
    expect(body).not.toContain("alice");
    expect(server.requestsTo("/sdk/v1/events")[0]?.headers.get("Authorization")).toBe(
      `Bearer ${server.key}`,
    );
  });

  test("a fallback before the first answer and a bootstrap hit record nothing", async () => {
    const { client, server } = start({ a: served(true, 1) }, { bootstrap: { a: true } });
    client.variation("a", false);
    client.variation("b", false);
    await client.ready();
    await client.close();
    expect(server.batches).toEqual([]);
  });

  test("sendUsage false sends nothing", async () => {
    const { client, server } = await started({ a: served(true, 1) }, { sendUsage: false });
    client.variation("a", false);
    await client.close();
    expect(server.requestsTo("/sdk/v1/events")).toHaveLength(0);
  });

  test("flushes on its interval while open", async () => {
    const { client, server, clock } = await started({ a: served(true, 1) });
    client.variation("a", false);
    await clock.advance(31_000);
    expect(server.batches.flat()).toHaveLength(1);
    await client.close();
  });

  test("nothing is recorded after close", async () => {
    const { client, server } = await started({ a: served(true, 1) });
    await client.close();
    client.variation("a", false);
    await client.close();
    expect(server.requestsTo("/sdk/v1/events")).toHaveLength(0);
  });
});
