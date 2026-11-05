import { describe, expect, it } from "vitest";
import { settle } from "./fake-clock.ts";
import { flag } from "./fake-server.ts";
import { useHarness, user } from "./harness.ts";

const h = useHarness();

describe("first load", () => {
  it("fetches the ruleset with the key and serves from it", async () => {
    const client = h.start();
    expect(await client.ready()).toBe(true);

    expect(client.variation("checkout", user, false)).toBe(true);
    expect(client.variation("legacy", user, true)).toBe(false);
    const [first] = h.server.requestsTo("/sdk/v1/ruleset");
    expect(first?.headers.get("Authorization")).toBe(`Bearer ${h.server.key}`);
    expect(first?.headers.get("If-None-Match")).toBeNull();
    expect(first?.headers.get("Authorization")).not.toContain(" undefined");
    expect(h.errors).toEqual([]);
  });

  it("answers variationDetail with the index and the reason", async () => {
    const client = h.start();
    await client.ready();
    expect(client.variationDetail("checkout", user, false)).toEqual({
      value: true,
      variationIndex: 1,
      reason: { kind: "FALLTHROUGH" },
    });
  });

  it("evaluates with the engine, rules and all", async () => {
    h.server.setFlags([
      {
        ...flag("beta", false),
        rules: [
          {
            clauses: [{ op: "in", attribute: "plan", values: ["pro"] }],
            variation: 1,
          },
        ],
      },
    ]);
    const client = h.start();
    await client.ready();
    expect(client.variation("beta", { key: "a", attributes: { plan: "pro" } }, false)).toBe(true);
    expect(client.variation("beta", { key: "b", attributes: { plan: "free" } }, true)).toBe(false);
    expect(
      client.variationDetail("beta", { key: "a", attributes: { plan: "pro" } }, false).reason,
    ).toEqual({
      kind: "RULE_MATCH",
      ruleIndex: 0,
    });
  });

  it("joins the url without doubling its slash", async () => {
    const client = h.start({ url: "https://flags.example.test///" });
    await client.ready();
    expect(h.server.seen[0]?.path).toBe("/sdk/v1/ruleset");
  });
});

describe("a ruleset that has not changed", () => {
  it("is asked for with If-None-Match and a 304 keeps what is held", async () => {
    const client = h.start({ stream: false });
    await client.ready();
    await h.clock.advance(30_000);

    const requests = h.server.requestsTo("/sdk/v1/ruleset");
    expect(requests).toHaveLength(2);
    expect(requests[1]?.headers.get("If-None-Match")).toBe(h.server.etag);
    expect(client.variation("checkout", user, false)).toBe(true);
    expect(h.errors).toEqual([]);
  });

  it("picks up a change on the next poll, then asks with the new tag", async () => {
    const client = h.start({ stream: false });
    await client.ready();
    h.server.setFlags([flag("checkout", false)]);
    await h.clock.advance(30_000);
    expect(client.variation("checkout", user, true)).toBe(false);

    await h.clock.advance(30_000);
    expect(h.server.requestsTo("/sdk/v1/ruleset")[2]?.headers.get("If-None-Match")).toBe('"v2"');
  });

  it("is not fetched again when the stream announces the tag already held", async () => {
    const client = h.start();
    await client.ready();
    await settle();
    expect(h.server.openStreams).toBe(1);
    expect(h.server.requestsTo("/sdk/v1/ruleset")).toHaveLength(1);
  });
});

describe("never throws at a caller", () => {
  it("returns the fallback for a bad context, and says why", async () => {
    const client = h.start();
    await client.ready();
    expect(client.variationDetail("checkout", { key: "" }, false)).toEqual({
      value: false,
      variationIndex: null,
      reason: { kind: "ERROR", code: "INVALID_CONTEXT" },
    });
    expect(client.variation("checkout", JSON.parse("null"), "x")).toBe("x");
    expect(client.variation("nope", user, 3)).toBe(3);
  });

  it("returns the fallback when the served value is not the type the caller expects", async () => {
    const client = h.start();
    await client.ready();
    expect(client.variationDetail("checkout", user, "yes")).toEqual({
      value: "yes",
      variationIndex: null,
      reason: { kind: "ERROR", code: "TYPE_MISMATCH" },
    });
  });

  it("lets a null fallback stand for any type", async () => {
    const client = h.start();
    await client.ready();
    expect(client.variationDetail<string | boolean | null>("checkout", user, null).value).toBe(
      true,
    );
  });

  it("returns the fallback when evaluating throws", async () => {
    const client = h.start();
    await client.ready();
    const hostile = {
      key: "user-1",
      get attributes(): never {
        throw new Error("boom");
      },
    };
    expect(client.variationDetail("checkout", hostile, false).reason).toEqual({
      kind: "ERROR",
      code: "EVALUATION_FAILED",
    });
  });

  it("returns the fallback for a flag that is off and has no off variation", async () => {
    h.server.setFlags([{ ...flag("dark", true), on: false, offVariation: null }]);
    const client = h.start();
    await client.ready();
    expect(client.variationDetail("dark", user, "fb")).toEqual({
      value: "fb",
      variationIndex: null,
      reason: { kind: "OFF" },
    });
  });

  it("does not block: it is synchronous and needs no ready()", () => {
    h.server.down = true;
    const client = h.start();
    expect(client.variation("checkout", user, "now")).toBe("now");
  });
});
