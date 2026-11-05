import { describe, expect, test } from "vitest";
import { ApiFailure, apiPath, createAdminApi, readConnection, type Fetch } from "./client.server";

const TOKEN = "unit-test-token-123";
const connection = { url: "http://api.test", token: TOKEN };

interface Seen {
  method: string;
  auth: string | null;
  body: unknown;
}

function fakeFetch(routes: Record<string, () => Response>) {
  const seen: Seen[] = [];
  const doFetch: Fetch = (input, init) => {
    const method = init.method ?? "GET";
    const body = typeof init.body === "string" ? JSON.parse(init.body) : undefined;
    seen.push({ method, auth: new Headers(init.headers).get("authorization"), body });
    const route = routes[`${method} ${new URL(input).pathname}`];
    return Promise.resolve(route === undefined ? new Response("", { status: 404 }) : route());
  };
  return { doFetch, seen };
}

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

const refused: Fetch = () => Promise.reject(new TypeError("fetch failed"));

/** The failure `GET /projects` ends in when the server answers with `response`. */
async function failureOf(response: () => Response): Promise<ApiFailure> {
  const { doFetch } = fakeFetch({ "GET /api/v1/projects": response });
  const error = await createAdminApi(connection, doFetch)
    .projects()
    .then(
      () => undefined,
      (e: unknown) => e,
    );
  if (!(error instanceof ApiFailure)) throw new Error("expected an ApiFailure");
  return error;
}

async function refusedFailure(): Promise<ApiFailure> {
  const error = await createAdminApi(connection, refused)
    .projects()
    .then(
      () => undefined,
      (e: unknown) => e,
    );
  if (!(error instanceof ApiFailure)) throw new Error("expected an ApiFailure");
  return error;
}

describe("apiPath", () => {
  test("encodes every segment so a key cannot change the path", () => {
    expect(apiPath("projects", "a/b", "x y", "?#")).toBe("/api/v1/projects/a%2Fb/x%20y/%3F%23");
  });
});

describe("flags", () => {
  test("joins each flag with its state in the environment and sends the bearer token", async () => {
    const { doFetch, seen } = fakeFetch({
      "GET /api/v1/projects/shop/flags": () =>
        json([
          { key: "a", name: "A", tags: ["x"], temporary: true, lastEvaluated: "2026-11-04" },
          { key: "b", name: "B", tags: [], temporary: false, lastEvaluated: null },
        ]),
      "GET /api/v1/projects/shop/flags/a/environments/prod": () => json({ on: true, version: 4 }),
      "GET /api/v1/projects/shop/flags/b/environments/prod": () => json({ on: false, version: 1 }),
    });
    const flags = await createAdminApi(connection, doFetch).flags("shop", "prod");
    expect(flags.map((f) => [f.key, f.on, f.version, f.temporary, f.lastEvaluated])).toEqual([
      ["a", true, 4, true, "2026-11-04"],
      ["b", false, 1, false, null],
    ]);
    expect(seen.every((s) => s.auth === `Bearer ${TOKEN}`)).toBe(true);
  });

  test("a flag with no config in the environment is unknown, not an error", async () => {
    const { doFetch } = fakeFetch({
      "GET /api/v1/projects/shop/flags": () => json([{ key: "a", name: "A" }]),
      "GET /api/v1/projects/shop/flags/a/environments/prod": () =>
        json({ error: { code: "not_found", message: "No config." } }, 404),
    });
    const [flag] = await createAdminApi(connection, doFetch).flags("shop", "prod");
    expect(flag?.on).toBeNull();
  });

  test("a config read that fails with a server error is a failure, not an unknown flag", async () => {
    const { doFetch } = fakeFetch({
      "GET /api/v1/projects/shop/flags": () => json([{ key: "a", name: "A" }]),
      "GET /api/v1/projects/shop/flags/a/environments/prod": () =>
        json({ error: { code: "internal", message: "Boom." } }, 500),
    });
    await expect(createAdminApi(connection, doFetch).flags("shop", "prod")).rejects.toMatchObject({
      kind: "error",
      status: 500,
    });
  });

  test("a server that dies mid-list is a failure, not an unknown flag", async () => {
    const { doFetch } = fakeFetch({
      "GET /api/v1/projects/shop/flags": () => json([{ key: "a", name: "A" }]),
      "GET /api/v1/projects/shop/flags/a/environments/prod": () =>
        new Response("", { status: 502 }),
    });
    await expect(createAdminApi(connection, doFetch).flags("shop", "prod")).rejects.toMatchObject({
      kind: "unreachable",
    });
  });
});

describe("writes", () => {
  test("toggle sends on and the version it was read at", async () => {
    const { doFetch, seen } = fakeFetch({
      "POST /api/v1/projects/shop/flags/a/environments/prod/toggle": () => json({ on: true }),
    });
    await createAdminApi(connection, doFetch).toggle({
      project: "shop",
      flag: "a",
      env: "prod",
      on: true,
      version: 4,
    });
    expect(seen[0]?.body).toEqual({ on: true, version: 4 });
  });

  test("create sends a boolean flag", async () => {
    const { doFetch, seen } = fakeFetch({
      "POST /api/v1/projects/shop/flags": () => json({}, 201),
    });
    await createAdminApi(connection, doFetch).createFlag("shop", {
      key: "k",
      name: "K",
      description: "",
      temporary: false,
      tags: ["t"],
    });
    expect(seen[0]?.body).toMatchObject({ key: "k", variations: [true, false], tags: ["t"] });
  });

  test("archive posts to the flag's archive route", async () => {
    const { doFetch, seen } = fakeFetch({
      "POST /api/v1/projects/shop/flags/a/archive": () => json({}),
    });
    await createAdminApi(connection, doFetch).archiveFlag("shop", "a");
    expect(seen[0]?.method).toBe("POST");
  });
});

describe("failures", () => {
  test("a refused connection is unreachable", async () => {
    expect((await refusedFailure()).kind).toBe("unreachable");
  });

  test("401 and 403 are unauthorized", async () => {
    expect((await failureOf(() => new Response("", { status: 401 }))).kind).toBe("unauthorized");
    expect((await failureOf(() => new Response("", { status: 403 }))).kind).toBe("unauthorized");
  });

  test("a 5xx with no error body is unreachable; with one it is the server's error", async () => {
    expect((await failureOf(() => new Response("bad gateway", { status: 502 }))).kind).toBe(
      "unreachable",
    );
    const error = await failureOf(() =>
      json({ error: { code: "boom", message: "It broke." } }, 500),
    );
    expect(error.kind).toBe("error");
    expect(error.message).toBe("boom: It broke.");
  });

  test("a 409 keeps its status and the server's message", async () => {
    const error = await failureOf(() =>
      json({ error: { code: "version_conflict", message: "Changed." } }, 409),
    );
    expect(error.status).toBe(409);
    expect(error.message).toContain("Changed.");
  });

  test("no failure message contains the token", async () => {
    const errors = [
      await refusedFailure(),
      await failureOf(() => new Response("", { status: 401 })),
      await failureOf(() => new Response("nope", { status: 500 })),
    ];
    for (const error of errors) expect(error.message).not.toContain(TOKEN);
  });
});

describe("readConnection", () => {
  test("accepts an http(s) url and a token, and drops a trailing slash", () => {
    expect(readConnection("http://localhost:5000/", "t")).toEqual({
      ok: true,
      connection: { url: "http://localhost:5000", token: "t" },
    });
  });

  test.each([
    [undefined, undefined],
    ["http://x", undefined],
    ["ftp://x", "secret-value"],
    ["not a url", "secret-value"],
  ])("says which variable is missing or wrong, never a value (%s, %s)", (url, token) => {
    const result = readConnection(url, token);
    expect(result.ok).toBe(false);
    expect(JSON.stringify(result)).toMatch(/FEATURESLASH_/);
    expect(JSON.stringify(result)).not.toContain("secret-value");
  });
});
