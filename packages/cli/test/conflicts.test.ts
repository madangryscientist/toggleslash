import { describe, expect, it } from "vitest";
import { P } from "./case.ts";
import { apiError, fakeServer } from "./fake-server.ts";
import { CONNECTED, cli } from "./harness.ts";

/** Runs one command against a server that answers 409 and returns what the CLI did. */
async function conflict(args: string[], route: string, message: string, stdin?: string) {
  const server = fakeServer({ [route]: apiError(409, "conflict", message) });
  const result = await cli(args, { fetch: server.fetch, env: CONNECTED, stdin });
  return {
    result,
    requests: server.seen.length,
    line: `toggleslash: the server answered 409 conflict: ${message}\n`,
  };
}

describe("a 409 exits 1 with the server's message", () => {
  it("targeting set, when the version is stale", async () => {
    const { result, requests, line } = await conflict(
      ["targeting", "set", "new-checkout", "-p", "shop", "-e", "production", "--file", "-"],
      `PUT ${P}/shop/flags/new-checkout/environments/production`,
      "The version is 3 but the config is at 4. Read it again.",
      JSON.stringify({ on: true, version: 3 }),
    );
    expect(result.code).toBe(1);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe(line);
    expect(requests).toBe(1);
  });

  it("segments set, when the version is stale", async () => {
    const { result, requests, line } = await conflict(
      ["segments", "set", "beta", "-p", "shop", "-e", "production", "--file", "-"],
      `PUT ${P}/shop/environments/production/segments/beta`,
      "The version is 2 but the segment is at 3. Read it again.",
      JSON.stringify({ included: [], excluded: [], rules: [], version: 2 }),
    );
    expect(result.code).toBe(1);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe(line);
    expect(requests).toBe(1);
  });

  it("segments delete, when a flag still uses the segment", async () => {
    const { result, requests, line } = await conflict(
      ["segments", "delete", "beta", "-p", "shop", "-e", "production"],
      `DELETE ${P}/shop/environments/production/segments/beta`,
      'Segment "beta" is used by flag "new-checkout".',
    );
    expect(result.code).toBe(1);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe(line);
    expect(requests).toBe(1);
  });
});
