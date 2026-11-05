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
