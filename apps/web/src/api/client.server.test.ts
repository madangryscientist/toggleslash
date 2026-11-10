import { describe, expect, test } from "vitest";
import { ApiFailure, apiPath, createAdminApi, readConnection, type Fetch } from "./client.server";

const TOKEN = "unit-test-token-123";
const connection = { url: "http://api.test", token: TOKEN };

interface Seen {
  method: string;
  search: string;
  auth: string | null;
  body: unknown;
}

function fakeFetch(routes: Record<string, () => Response>) {
  const seen: Seen[] = [];
  const doFetch: Fetch = (input, init) => {
    const method = init.method ?? "GET";
    const body = typeof init.body === "string" ? JSON.parse(init.body) : undefined;
    seen.push({
      method,
      search: new URL(input).search,
      auth: new Headers(init.headers).get("authorization"),
      body,
    });
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
  const flagsPath = "GET /api/v1/projects/shop/flags";
  const statesPath = "GET /api/v1/projects/shop/environments/prod/flag-states";

  test("joins each flag with its state in the environment and sends the bearer token", async () => {
    const { doFetch, seen } = fakeFetch({
      [flagsPath]: () =>
        json([
          { key: "a", name: "A", tags: ["x"], temporary: true, lastEvaluated: "2026-01-01" },
          { key: "b", name: "B", tags: [], temporary: false, lastEvaluated: "2026-01-02" },
        ]),
      [statesPath]: () =>
        json([
          { flag: "b", on: false, version: 1, lastEvaluated: null },
          { flag: "a", on: true, version: 4, lastEvaluated: "2026-11-04" },
        ]),
    });
    const flags = await createAdminApi(connection, doFetch).flags("shop", "prod");
    // The column is the environment's day, not the flag's any-environment day.
    expect(flags.map((f) => [f.key, f.on, f.version, f.temporary, f.lastEvaluated])).toEqual([
      ["a", true, 4, true, "2026-11-04"],
      ["b", false, 1, false, null],
    ]);
    expect(seen.every((s) => s.auth === `Bearer ${TOKEN}`)).toBe(true);
  });

  test("makes exactly two calls however many flags there are", async () => {
    const keys = Array.from({ length: 60 }, (_, i) => `flag-${i}`);
    const { doFetch, seen } = fakeFetch({
      [flagsPath]: () => json(keys.map((key) => ({ key, name: key }))),
      [statesPath]: () =>
        json(keys.map((flag) => ({ flag, on: true, version: 1, lastEvaluated: null }))),
    });
    const flags = await createAdminApi(connection, doFetch).flags("shop", "prod");
    expect(flags).toHaveLength(60);
    expect(seen).toHaveLength(2);
    expect(flags.every((f) => f.on === true)).toBe(true);
  });

  test("a flag missing from the states is unknown, not an error", async () => {
    const { doFetch } = fakeFetch({
      [flagsPath]: () =>
        json([
          { key: "a", name: "A", lastEvaluated: "2026-11-04" },
          { key: "b", name: "B" },
        ]),
      [statesPath]: () => json([{ flag: "b", on: true, version: 2, lastEvaluated: null }]),
    });
    const [a, b] = await createAdminApi(connection, doFetch).flags("shop", "prod");
    expect([a?.on, a?.version, a?.lastEvaluated]).toEqual([null, null, null]);
    expect([b?.on, b?.version]).toEqual([true, 2]);
  });

  test("states the server does not have for the environment (404) leave every flag unknown", async () => {
    const { doFetch } = fakeFetch({
      [flagsPath]: () => json([{ key: "a", name: "A" }]),
      [statesPath]: () =>
        json({ error: { code: "environment_not_found", message: "No environment." } }, 404),
    });
    const [flag] = await createAdminApi(connection, doFetch).flags("shop", "prod");
    expect([flag?.key, flag?.on, flag?.version, flag?.lastEvaluated]).toEqual([
      "a",
      null,
      null,
      null,
    ]);
  });

  test("a states read that fails with a server error is a failure, not an unknown flag", async () => {
    const { doFetch } = fakeFetch({
      [flagsPath]: () => json([{ key: "a", name: "A" }]),
      [statesPath]: () => json({ error: { code: "internal", message: "Boom." } }, 500),
    });
    await expect(createAdminApi(connection, doFetch).flags("shop", "prod")).rejects.toMatchObject({
      kind: "error",
      status: 500,
    });
  });

  test("a server that dies mid-list is a failure, not an unknown flag", async () => {
    const { doFetch } = fakeFetch({
      [flagsPath]: () => json([{ key: "a", name: "A" }]),
      [statesPath]: () => new Response("", { status: 502 }),
    });
    await expect(createAdminApi(connection, doFetch).flags("shop", "prod")).rejects.toMatchObject({
      kind: "unreachable",
    });
  });
});

describe("lifecycle", () => {
  const flagsPath = "GET /api/v1/projects/shop/flags";
  const statesPath = "GET /api/v1/projects/shop/environments/prod/flag-states";

  test("the list reads each flag's stage, and an unknown or missing word is null", async () => {
    const { doFetch } = fakeFetch({
      [flagsPath]: () =>
        json([
          { key: "a", name: "A", lifecycle: "new" },
          { key: "b", name: "B", lifecycle: "active" },
          { key: "c", name: "C", lifecycle: "inactive" },
          { key: "d", name: "D", lifecycle: "archived" },
          { key: "e", name: "E", lifecycle: "stale" },
          { key: "f", name: "F" },
        ]),
      [statesPath]: () => json([]),
    });
    const flags = await createAdminApi(connection, doFetch).flags("shop", "prod");
    expect(flags.map((f) => f.lifecycle)).toEqual([
      "new",
      "active",
      "inactive",
      "archived",
      null,
      null,
    ]);
  });

  test("the detail reads the stage, and an unknown or missing word is null", async () => {
    const answers = [{ lifecycle: "inactive" }, { lifecycle: 3 }, {}];
    const stages = [];
    for (const extra of answers) {
      const { doFetch } = fakeFetch({
        "GET /api/v1/projects/shop/flags/a": () => json({ key: "a", name: "A", ...extra }),
      });
      stages.push((await createAdminApi(connection, doFetch).flag("shop", "a")).lifecycle);
    }
    expect(stages).toEqual(["inactive", null, null]);
  });
});

describe("archived flags", () => {
  test("flags with archived lists ?archived=true and reads no states", async () => {
    const { doFetch, seen } = fakeFetch({
      "GET /api/v1/projects/shop/flags": () =>
        json([{ key: "a", name: "A", lifecycle: "archived", archivedAt: "2026-11-01T00:00:00Z" }]),
    });
    const flags = await createAdminApi(connection, doFetch).flags("shop", "prod", {
      archived: true,
    });
    expect(seen.map((s) => s.search)).toEqual(["?archived=true"]);
    expect(flags.map((f) => [f.key, f.lifecycle, f.on, f.version, f.lastEvaluated])).toEqual([
      ["a", "archived", null, null, null],
    ]);
  });

  test("restore posts to the flag's restore route", async () => {
    const { doFetch, seen } = fakeFetch({
      "POST /api/v1/projects/shop/flags/a/restore": () => json({}),
    });
    await createAdminApi(connection, doFetch).restoreFlag("shop", "a");
    expect(seen.map((s) => s.method)).toEqual(["POST"]);
  });

  test("delete sends DELETE to the flag, and a 409 keeps the server's message", async () => {
    const { doFetch, seen } = fakeFetch({
      "DELETE /api/v1/projects/shop/flags/a": () => new Response(null, { status: 204 }),
      "DELETE /api/v1/projects/shop/flags/b": () =>
        json({ error: { code: "conflict", message: '"b" is a prerequisite of "a".' } }, 409),
    });
    const api = createAdminApi(connection, doFetch);
    await api.deleteFlag("shop", "a");
    await expect(api.deleteFlag("shop", "b")).rejects.toMatchObject({ status: 409 });
    expect(seen.map((s) => s.method)).toEqual(["DELETE", "DELETE"]);
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
    expect(JSON.stringify(result)).toMatch(/TOGGLESLASH_/);
    expect(JSON.stringify(result)).not.toContain("secret-value");
  });
});
