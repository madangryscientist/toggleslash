import { describe, expect, it } from "vitest";
import { P } from "./case.ts";
import { HASH, snapshot } from "./ruleset-cases.ts";
import { fakeServer, json } from "./fake-server.ts";
import { CONNECTED, cli } from "./harness.ts";

const ROUTE = `GET ${P}/shop/environments/production/rulesets/${HASH}`;
const ARGS = ["rulesets", "show", HASH, "-p", "shop", "-e", "production"];

async function run(args: string[], response = json(200, snapshot)) {
  const server = fakeServer({ [ROUTE]: response });
  const result = await cli(args, { fetch: server.fetch, env: CONNECTED });
  return { ...result, seen: server.seen };
}

describe("rulesets show", () => {
  it("prints the tag, the time, the flags and the segments", async () => {
    const result = await run(ARGS);
    expect(result.code).toBe(0);
    expect(result.stdout).toBe(
      [
        `etag:           "${HASH}"`,
        "firstServedAt:  2026-11-05T18:00:00Z",
        "",
        "FLAG          ON     OFF  FALLTHROUGH      VARIATIONS    TARGETS  PREREQUISITES  RULES  VERSION",
        'new-checkout  false  1    {"variation":0}  [true,false]  0        0              1      4',
        "",
        "SEGMENT  INCLUDED  EXCLUDED  RULES",
        "beta     2         0         0",
        "",
      ].join("\n"),
    );
    expect(result.seen[0]?.query).toBe("");
  });

  it("sends --flag as the query", async () => {
    const result = await run([...ARGS, "--flag", "new-checkout"]);
    expect(result.code).toBe(0);
    expect(result.seen[0]?.query).toBe("?flag=new-checkout");
  });

  it("leaves the segment table out when there are no segments", async () => {
    const result = await run(ARGS, json(200, { ...snapshot, segments: [] }));
    expect(result.code).toBe(0);
    expect(result.stdout).not.toContain("SEGMENT");
  });

  it("exits 2 and sends no request for a hash in its ETag quotes", async () => {
    const result = await run(["rulesets", "show", `"${HASH}"`, "-p", "shop", "-e", "production"]);
    expect(result.code).toBe(2);
    expect(result.stdout).toBe("");
    expect(result.stderr).toContain("lowercase hex");
    expect(result.seen).toEqual([]);
  });

  it("exits 2 and sends no request without a hash or --env", async () => {
    for (const args of [
      ["rulesets", "show", "-p", "shop", "-e", "production"],
      ["rulesets", "show", HASH, "-p", "shop"],
    ]) {
      const result = await run(args);
      expect(result.code).toBe(2);
      expect(result.seen).toEqual([]);
    }
  });
});
