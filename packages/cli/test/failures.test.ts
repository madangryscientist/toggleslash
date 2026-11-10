import { describe, expect, it } from "vitest";
import { P, config, flag } from "./case.ts";
import { apiError, fakeServer, json } from "./fake-server.ts";
import { CONNECTED, cli } from "./harness.ts";

describe("flags show --env", () => {
  const routes = {
    [`GET ${P}/shop/flags/new-checkout`]: json(200, flag),
    [`GET ${P}/shop/flags/new-checkout/environments/production`]: json(200, config),
  };

  it("adds the flag's targeting in that environment", async () => {
    const server = fakeServer(routes);
    const result = await cli(["flags", "show", "new-checkout", "-p", "shop", "-e", "production"], {
      fetch: server.fetch,
      env: CONNECTED,
    });
    expect(result.code).toBe(0);
    expect(result.stdout).toContain("production on:");
    expect(result.stdout).toContain("production version:");
    expect(result.stdout).toContain("production rules:");
  });

  it("nests the targeting under config in --json", async () => {
    const server = fakeServer(routes);
    const result = await cli(
      ["flags", "show", "new-checkout", "-p", "shop", "-e", "production", "--json"],
      {
        fetch: server.fetch,
        env: CONNECTED,
      },
    );
    expect(JSON.parse(result.stdout)).toEqual({ ...flag, config });
  });
});

describe("server and network failures", () => {
  const args = ["projects", "list"];

  it("exits 1 for a 401 and says to fix the token without printing it", async () => {
    const server = fakeServer({ [`GET ${P}`]: apiError(401, "unauthorized", "no") });
    const result = await cli(args, { fetch: server.fetch, env: CONNECTED });
    expect(result.code).toBe(1);
    expect(result.stderr).toContain("rejected the admin token (401)");
  });

  it("exits 1 for a 500, a 503 with no body and a 302", async () => {
    for (const reply of [
      apiError(500, "internal", "boom"),
      new Response("", { status: 503 }),
      new Response(null, { status: 302, headers: { location: "https://elsewhere.test/" } }),
    ]) {
      const server = fakeServer({ [`GET ${P}`]: reply });
      const result = await cli(args, { fetch: server.fetch, env: CONNECTED });
      expect(result.code).toBe(1);
      expect(result.stderr).toMatch(/^toggleslash: the server answered (500|503|302)/);
    }
  });

  it("exits 1 for a 200 whose body is not JSON, or not the expected shape", async () => {
    for (const reply of [new Response("<html>", { status: 200 }), json(200, { not: "a list" })]) {
      const server = fakeServer({ [`GET ${P}`]: reply });
      const result = await cli(args, { fetch: server.fetch, env: CONNECTED });
      expect(result.code).toBe(1);
      expect(result.stdout).toBe("");
    }
  });

  it("refuses to follow a redirect, so the token goes nowhere else", async () => {
    let redirect: RequestInit["redirect"];
    const result = await cli(args, {
      env: CONNECTED,
      fetch: async (_url, init) => {
        redirect = init.redirect;
        return new Response(null, { status: 307 });
      },
    });
    expect(redirect).toBe("manual");
    expect(result.code).toBe(1);
  });

  it("encodes a key so it cannot reach another route", async () => {
    const server = fakeServer({});
    await cli(["flags", "show", "../../keys", "-p", "shop"], {
      fetch: server.fetch,
      env: CONNECTED,
    });
    expect(server.seen[0]?.path).toBe(`${P}/shop/flags/..%2F..%2Fkeys`);
  });

  it("uses --url over TOGGLESLASH_URL", async () => {
    let called = "";
    await cli(["projects", "list", "--url", "http://localhost:5000/"], {
      env: CONNECTED,
      fetch: async (input) => {
        called = input;
        return json(200, []);
      },
    });
    expect(called).toBe("http://localhost:5000/api/v1/projects");
  });
});

describe("usage errors exit 2 and call nothing", () => {
  const usage: [string, string[], string][] = [
    ["a missing --project", ["flags", "list"], "--project is required"],
    ["a missing --env", ["flags", "on", "f", "--project", "p"], "--env is required"],
    ["a missing flag key", ["flags", "show", "--project", "p"], "takes the flag key"],
    ["an extra argument", ["projects", "list", "extra"], "takes no arguments"],
    ["an unknown option", ["projects", "list", "--bogus"], "projects list:"],
    ["a bad --archived", ["flags", "list", "-p", "p", "--archived", "maybe"], "--archived must be"],
    [
      "bad --variations JSON",
      ["flags", "create", "f", "-p", "p", "--variations", "[1,"],
      "--variations must be a JSON array",
    ],
    [
      "--variations that is not an array",
      ["flags", "create", "f", "-p", "p", "--variations", "{}"],
      "--variations must be a JSON array",
    ],
    [
      "a non-numeric --off-variation",
      ["flags", "create", "f", "-p", "p", "--off-variation", "x"],
      "--off-variation must be a whole number",
    ],
    [
      "a non-numeric --limit",
      ["flags", "history", "f", "-p", "p", "--limit", "x"],
      "--limit must be a whole number",
    ],
    [
      "a bad --kind",
      ["keys", "create", "-p", "p", "-e", "e", "--kind", "admin", "--name", "n"],
      '--kind must be "server" or "client"',
    ],
    [
      "a missing --name on keys create",
      ["keys", "create", "-p", "p", "-e", "e", "--kind", "server"],
      "--name is required",
    ],
    [
      "a non-numeric key id",
      ["keys", "revoke", "abc", "-p", "p", "-e", "e"],
      "the key id is the number",
    ],
    [
      "a bad --permission",
      [
        "tokens",
        "create",
        "-p",
        "p",
        "--name",
        "n",
        "--permission",
        "admin",
        "--expires-in-days",
        "1",
      ],
      '--permission must be "read" or "write"',
    ],
    [
      "a missing --expires-in-days",
      ["tokens", "create", "-p", "p", "--name", "n", "--permission", "read"],
      "--expires-in-days is required",
    ],
    [
      "a missing --name on envs rename",
      ["envs", "rename", "prod", "-p", "p"],
      "--name is required",
    ],
    [
      "a blank --name on envs rename",
      ["envs", "rename", "prod", "-p", "p", "--name", "  "],
      "--name is required",
    ],
    [
      "a missing key on envs rename",
      ["envs", "rename", "-p", "p", "--name", "n"],
      "takes the environment key",
    ],
    [
      "envs delete without --yes",
      ["envs", "delete", "prod", "-p", "p"],
      "pass --yes to delete prod",
    ],
    [
      "a missing key on envs delete",
      ["envs", "delete", "-p", "p", "--yes"],
      "takes the environment key",
    ],
    ["a group with no action", ["flags"], "flags needs one of"],
    ["an unknown action", ["flags", "frobnicate"], 'unknown command "flags frobnicate"'],
    ["an unknown command", ["nope"], 'unknown argument "nope"'],
  ];

  it.each(usage)("%s", async (_name, args, message) => {
    const server = fakeServer({});
    const result = await cli(args, { fetch: server.fetch, env: CONNECTED });
    expect(result.code).toBe(2);
    expect(result.stdout).toBe("");
    expect(result.stderr).toContain(message);
    expect(server.seen).toEqual([]);
  });
});

describe("envs delete", () => {
  it("delete exits 1 with the server's message when a live key blocks it", async () => {
    const server = fakeServer({
      [`DELETE ${P}/p/environments/prod`]: apiError(
        409,
        "conflict",
        'Environment "prod" still has 1 SDK key that is not revoked. Revoke it first.',
      ),
    });
    const result = await cli(["envs", "delete", "prod", "-p", "p", "--yes"], {
      fetch: server.fetch,
      env: CONNECTED,
    });
    expect(result.code).toBe(1);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe(
      'toggleslash: the server answered 409 conflict: Environment "prod" still has 1 SDK key that is not revoked. Revoke it first.\n',
    );
    expect(server.seen).toHaveLength(1);
  });
});

describe("flags delete", () => {
  it("exits 1 with the server's message when the flag is not archived", async () => {
    const server = fakeServer({
      [`DELETE ${P}/shop/flags/new-checkout`]: apiError(
        409,
        "conflict",
        '"new-checkout" is not archived; archive it before deleting it.',
      ),
    });
    const result = await cli(["flags", "delete", "new-checkout", "-p", "shop"], {
      fetch: server.fetch,
      env: CONNECTED,
    });
    expect(result.code).toBe(1);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe(
      'toggleslash: the server answered 409 conflict: "new-checkout" is not archived; archive it before deleting it.\n',
    );
    expect(server.seen).toHaveLength(1);
  });
});

describe("flags list STATE", () => {
  it("falls back to active or archived when an older server sends no lifecycle", async () => {
    const { lifecycle: _lifecycle, ...older } = flag;
    const server = fakeServer({
      [`GET ${P}/shop/flags`]: json(200, [
        older,
        { ...older, key: "gone", archivedAt: "2026-11-04T00:00:00Z" },
      ]),
    });
    const result = await cli(["flags", "list", "-p", "shop"], {
      fetch: server.fetch,
      env: CONNECTED,
    });
    expect(result.code).toBe(0);
    expect(result.stdout).toMatch(/new-checkout .* active /);
    expect(result.stdout).toMatch(/gone .* archived /);
  });
});
