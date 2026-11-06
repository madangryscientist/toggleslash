import { describe, expect, it } from "vitest";
import { COMMANDS } from "../src/commands/index.ts";
import { cases as otherCases } from "./cases.ts";
import { envCases } from "./env-cases.ts";
import { rulesetCases } from "./ruleset-cases.ts";
import { runtimeCases } from "./runtime-cases.ts";
import { tokenCases } from "./token-cases.ts";
import { apiError, fakeServer } from "./fake-server.ts";
import { CONNECTED, TOKEN, URL_BASE, cli } from "./harness.ts";

const cases = [...otherCases, ...envCases, ...tokenCases, ...runtimeCases, ...rulesetCases];

describe("every command that calls the server", () => {
  it.each(cases)("$command: sends the request and prints the result", async (c) => {
    const server = fakeServer(c.routes);
    const result = await cli(c.args, { fetch: server.fetch, env: CONNECTED });
    expect(result.stderr).not.toContain(TOKEN);
    expect(result.code).toBe(0);
    for (const text of c.shows) expect(result.stdout).toContain(text);
    expect(server.seen).toHaveLength(1);
    const [seen] = server.seen;
    expect(seen?.method).toBe(c.method);
    expect(`${URL_BASE}${seen?.path}`).toBe(`${URL_BASE}${c.path}`);
    expect(seen?.query).toBe(c.query ?? "");
    expect(seen?.headers["authorization"]).toBe(`Bearer ${TOKEN}`);
    expect(seen?.body).toEqual(c.body ?? seen?.body);
  });

  it.each(cases)("$command --json: prints the server's reply as valid JSON", async (c) => {
    const server = fakeServer(c.routes);
    const result = await cli([...c.args, "--json"], { fetch: server.fetch, env: CONNECTED });
    expect(result.code).toBe(0);
    expect(JSON.parse(result.stdout)).toEqual(c.reply);
  });

  it.each(cases)("$command: exits 1 when the server answers an error", async (c) => {
    const server = fakeServer({
      [`${c.method} ${c.path}`]: apiError(404, "not_found", "No such thing."),
    });
    const result = await cli(c.args, { fetch: server.fetch, env: CONNECTED });
    expect(result.code).toBe(1);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("toggleslash: the server answered 404 not_found: No such thing.\n");
  });

  it.each(cases)("$command: exits 1 when the server cannot be reached", async (c) => {
    const result = await cli(c.args, {
      env: CONNECTED,
      fetch: () => Promise.reject(new Error("connect ECONNREFUSED 127.0.0.1:1")),
    });
    expect(result.code).toBe(1);
    expect(result.stderr).toBe(
      `toggleslash: could not reach ${URL_BASE}: connect ECONNREFUSED 127.0.0.1:1\n`,
    );
  });

  it.each(cases)("$command: exits 2 with no URL and token configured", async (c) => {
    const result = await cli(c.args, { fetch: fakeServer({}).fetch });
    expect(result.code).toBe(2);
    expect(result.stderr).toContain("no server URL");
  });

  it("has a case for every command that calls the server", () => {
    const tested = new Set(cases.map((c) => c.command));
    const all = COMMANDS.filter((c) => c.api).map((c) => c.name);
    expect(all.filter((name) => !tested.has(name))).toEqual([]);
  });
});
