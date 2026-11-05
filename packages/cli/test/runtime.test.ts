import { describe, expect, it } from "vitest";
import { P } from "./cases.ts";
import { runtime } from "./runtime-cases.ts";
import { fakeServer, json } from "./fake-server.ts";
import { CONNECTED, TOKEN, cli } from "./harness.ts";

const ROUTE = `GET ${P}/shop/environments/production/runtime`;
const ARGS = ["runtime", "-p", "shop", "-e", "production"];

async function run(args: string[], response = json(200, runtime)) {
  const server = fakeServer({ [ROUTE]: response });
  const result = await cli(args, { fetch: server.fetch, env: CONNECTED });
  return { ...result, seen: server.seen };
}

describe("runtime", () => {
  it("prints the tag, the count, the time and the last change", async () => {
    const result = await run(ARGS);
    expect(result.code).toBe(0);
    expect(result.stdout).toBe(
      [
        'etag:         "9f2c"',
        "flagCount:    12",
        "generatedAt:  2026-11-05T18:00:00Z",
        "lastChange:   2026-11-05T17:58:00Z  token:3  toggled  flag_config  new-checkout/production",
        "",
      ].join("\n"),
    );
    expect(result.stderr).toBe("");
    expect(result.seen).toHaveLength(1);
  });

  it("prints - when nothing has changed the environment", async () => {
    const result = await run(ARGS, json(200, { ...runtime, lastChange: null }));
    expect(result.code).toBe(0);
    expect(result.stdout).toContain("lastChange:   -\n");
  });

  it("never prints the token, even when the server echoes it", async () => {
    const result = await run(ARGS, json(200, { ...runtime, etag: `"${TOKEN}"` }));
    expect(result.stdout + result.stderr).not.toContain(TOKEN);
  });

  it("exits 2 and sends no request without --env", async () => {
    const result = await run(["runtime", "-p", "shop"]);
    expect(result.code).toBe(2);
    expect(result.stdout).toBe("");
    expect(result.stderr).toContain("--env");
    expect(result.seen).toEqual([]);
  });
});
