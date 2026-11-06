import { describe, expect, test } from "vitest";
import { ApiFailure, createAdminApi, type Fetch } from "./client.server";
import { AUDIT_PAGE_SIZE } from "./operator.server";

const connection = { url: "http://api.test", token: "unit-test-token-123" };

function fakeFetch(routes: Record<string, () => Response>) {
  const seen: string[] = [];
  const doFetch: Fetch = (input, init) => {
    const url = new URL(input);
    const method = init.method ?? "GET";
    seen.push(url.pathname + url.search);
    const route = routes[`${method} ${url.pathname}`];
    return Promise.resolve(route === undefined ? new Response("", { status: 404 }) : route());
  };
  return { api: createAdminApi(connection, doFetch), seen };
}

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

describe("SDK keys", () => {
  const path = "/api/v1/projects/shop/environments/production/keys";

  test("an answer with no key is a failure, not a key with no secret", async () => {
    const { api } = fakeFetch({
      [`POST ${path}`]: () => json({ id: 3, kind: "server", name: "W" }),
    });
    await expect(api.createKey("shop", "production", "server", "W")).rejects.toBeInstanceOf(
      ApiFailure,
    );
  });
});

describe("audit", () => {
  test("asks for a page with only the filters that are set, and a day as midnight UTC", async () => {
    const { api, seen } = fakeFetch({
      "GET /api/v1/projects/shop/audit": () => json({ events: [], nextCursor: null }),
    });
    await api.audit("shop", { flag: "dark-mode", environment: "", since: "2026-10-20" }, "42");
    const query = new URL(`http://x${seen[0]}`).searchParams;
    expect(Object.fromEntries(query)).toEqual({
      limit: String(AUDIT_PAGE_SIZE),
      flag: "dark-mode",
      since: "2026-10-20T00:00:00Z",
      cursor: "42",
    });
  });
});

describe("ruleset at a hash", () => {
  const hash = "a".repeat(64);
  const path = `/api/v1/projects/shop/environments/production/rulesets/${hash}`;
  const served = {
    etag: `"${hash}"`,
    firstServedAt: "2026-11-02T09:30:00Z",
    flags: [
      {
        key: "checkout-v2",
        variations: [true, "blue"],
        on: true,
        offVariation: 1,
        targets: [],
        prerequisites: [],
        rules: [],
        fallthrough: { variation: 0 },
        version: 3,
        salt: "must-not-come-through",
      },
    ],
    segments: [{ key: "beta", included: ["u1"], excluded: [], rules: [], salt: "nor-this" }],
  };

  test("reads the flags with their variations, and drops every field the page does not show", async () => {
    const { api } = fakeFetch({ [`GET ${path}`]: () => json(served) });
    const answer = await api.ruleset("shop", "production", hash, null);
    expect(answer.ok).toBe(true);
    if (!answer.ok) return;
    expect(answer.ruleset.hash).toBe(hash);
    expect(answer.ruleset.flags[0]?.variations).toEqual(["true", '"blue"']);
    expect(answer.ruleset.flags[0]?.config.version).toBe(3);
    expect(JSON.stringify(answer)).not.toContain("must-not-come-through");
    expect(JSON.stringify(answer)).not.toContain("nor-this");
  });

  test("passes ?flag= along, encoded", async () => {
    const { api, seen } = fakeFetch({ [`GET ${path}`]: () => json(served) });
    await api.ruleset("shop", "production", hash, "a b&c");
    expect(seen[0]).toBe(`${path}?flag=a%20b%26c`);
  });

  test("encodes a hash that is not hex, so it cannot climb out of its place in the path", async () => {
    const { api, seen } = fakeFetch({});
    await api.ruleset("shop", "production", "../keys", null);
    expect(seen[0]).toBe("/api/v1/projects/shop/environments/production/rulesets/..%2Fkeys");
  });

  test("a hash the environment never served is a value with the server's words, not a throw", async () => {
    const { api } = fakeFetch({
      [`GET ${path}`]: () =>
        json({ error: { code: "ruleset_not_found", message: "Never served." } }, 404),
    });
    expect(await api.ruleset("shop", "production", hash, null)).toEqual({
      ok: false,
      message: "Never served.",
    });
  });

  test("any other failure still throws, so the page says the server is down", async () => {
    const { api } = fakeFetch({ [`GET ${path}`]: () => new Response("", { status: 401 }) });
    await expect(api.ruleset("shop", "production", hash, null)).rejects.toBeInstanceOf(ApiFailure);
  });
});
