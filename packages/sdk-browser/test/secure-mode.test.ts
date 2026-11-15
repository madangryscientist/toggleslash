import { describe, expect, test } from "vitest";
import { settle } from "./fake-clock.ts";
import { served } from "./fake-server.ts";
import { start, started } from "./harness.ts";

const EVALUATE = "/sdk/v1/evaluate";

function signatures(
  entries: Record<string, string>,
): (server: { signatures: Map<string, string> | null }) => void {
  return (server) => {
    server.signatures = new Map(Object.entries(entries));
  };
}

describe("secure mode", () => {
  test("sends the signature with every evaluate for the context", async () => {
    const { client, server } = start(
      { a: served(1, 0) },
      { signature: "sig-alice" },
      signatures({ alice: "sig-alice" }),
    );
    await client.ready();
    server.publish();
    await settle();
    const bodies = server.requestsTo(EVALUATE).map((seen) => seen.body);
    expect(bodies.length).toBeGreaterThanOrEqual(2);
    for (const body of bodies)
      expect(body).toEqual({ context: { key: "alice" }, signature: "sig-alice" });
    expect(client.variation("a", 0)).toBe(1);
    await client.close();
  });

  test("sends the new signature after identify, and not the old one", async () => {
    const { client, server } = start(
      { a: served(1, 0) },
      { signature: "sig-alice" },
      signatures({ alice: "sig-alice", bob: "sig-bob" }),
    );
    await client.ready();
    await client.identify({ key: "bob" }, "sig-bob");
    expect(server.requestsTo(EVALUATE).at(-1)?.body).toEqual({
      context: { key: "bob" },
      signature: "sig-bob",
    });
    await client.identify({ key: "carol" });
    expect(server.requestsTo(EVALUATE).at(-1)?.body).toEqual({ context: { key: "carol" } });
    await client.close();
  });

  test("a 403 for the signature is an error like any other: the cache serves and the client keeps going", async () => {
    const { client, server, errors } = start(
      { a: served(1, 0) },
      { signature: "sig-alice" },
      signatures({ alice: "sig-alice", bob: "sig-bob" }),
    );
    await client.ready();
    expect(client.variation("a", 0)).toBe(1);

    await client.identify({ key: "bob" }); // no signature
    await client.identify({ key: "bob" }, "wrong");
    expect(errors.map(String)).toEqual([
      "Error: POST /sdk/v1/evaluate answered 403 signature_required",
      "Error: POST /sdk/v1/evaluate answered 403 bad_signature",
    ]);
    expect(client.variation("a", 0)).toBe(1); // alice's answer still serves

    // Not halted: the right signature is answered, and the signal still runs.
    await client.identify({ key: "bob" }, "sig-bob");
    expect(errors).toHaveLength(2);
    const before = server.requestsTo(EVALUATE).length;
    server.publish();
    await settle();
    expect(server.requestsTo(EVALUATE).length).toBe(before + 1);
    await client.close();
  });

  test("before any answer, a refused signature serves the fallback", async () => {
    const { client, errors } = start({ a: served(1, 0) }, {}, signatures({ alice: "sig-alice" }));
    await client.ready();
    expect(client.variation("a", 7)).toBe(7);
    expect(errors).toHaveLength(1);
    await client.close();
  });

  test("any other 403 on evaluate still stops the client", async () => {
    const { client, server, errors, clock } = await started(
      { a: served(1, 0) },
      { pollIntervalMs: 5_000 },
    );
    server.statuses.set(EVALUATE, { status: 403 });
    server.publish();
    await settle();
    const count = server.seen.length;
    await clock.advance(200_000);
    expect(server.seen).toHaveLength(count);
    expect(errors).toHaveLength(1);
    await client.close();
  });
});
