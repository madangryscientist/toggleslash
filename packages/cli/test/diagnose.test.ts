import { writeFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { P, diagnosis } from "./case.ts";
import { apiError, fakeServer, json } from "./fake-server.ts";
import { CONNECTED, cli, tempDir } from "./harness.ts";

const ROUTE = `POST ${P}/shop/environments/production/evaluate`;
const BASE = ["diagnose", "new-checkout", "-p", "shop", "-e", "production"];

function reply(reason: unknown, value: unknown = true, variationIndex: unknown = 0) {
  return json(200, { flags: { "new-checkout": { value, variationIndex, reason } } });
}

async function run(args: string[], response = json(200, diagnosis)) {
  const server = fakeServer({ [ROUTE]: response });
  const result = await cli(args, { fetch: server.fetch, env: CONNECTED });
  return { ...result, seen: server.seen };
}

describe("diagnose: the context", () => {
  it("sends --context as the request's context, with the flag key", async () => {
    const result = await run([...BASE, "--context", '{"key":"u1","attributes":{"plan":"pro"}}']);
    expect(result.code).toBe(0);
    expect(result.seen).toHaveLength(1);
    expect(result.seen[0]?.body).toEqual({
      flagKey: "new-checkout",
      context: { key: "u1", attributes: { plan: "pro" } },
    });
  });

  it("reads --context-file", async () => {
    const file = join(tempDir(), "context.json");
    writeFileSync(file, '{ "key": "u2" }\n');
    const result = await run([...BASE, "--context-file", file]);
    expect(result.code).toBe(0);
    expect(result.seen[0]?.body).toEqual({ flagKey: "new-checkout", context: { key: "u2" } });
  });

  it("uses -p and -e like every command", async () => {
    const result = await run([
      "diagnose",
      "new-checkout",
      "--project",
      "shop",
      "--env",
      "production",
      "--context",
      "{}",
    ]);
    expect(result.code).toBe(0);
    expect(result.seen[0]?.path).toBe(`${P}/shop/environments/production/evaluate`);
  });
});

describe("diagnose: --etag", () => {
  const HASH = "a".repeat(64);

  it("sends the hash as the request's etag", async () => {
    const result = await run([...BASE, "--context", '{"key":"u1"}', "--etag", HASH]);
    expect(result.code).toBe(0);
    expect(result.seen[0]?.body).toEqual({
      flagKey: "new-checkout",
      context: { key: "u1" },
      etag: HASH,
    });
  });

  it("sends no etag field without the option", async () => {
    const result = await run([...BASE, "--context", '{"key":"u1"}']);
    expect(result.seen[0]?.body).not.toHaveProperty("etag");
  });

  it.each(["", "ABCDEF", `"${"a".repeat(64)}"`, "a".repeat(63)])(
    "refuses %j before any request, as a usage error",
    async (etag) => {
      const result = await run([...BASE, "--context", "{}", "--etag", etag]);
      expect(result.code).toBe(2);
      expect(result.stderr).toContain("64 lowercase hex");
      expect(result.seen).toEqual([]);
    },
  );

  it("exits 1 for a hash the environment never served", async () => {
    const result = await run(
      [...BASE, "--context", "{}", "--etag", HASH],
      apiError(404, "not_found", "This environment never served a ruleset with that hash."),
    );
    expect(result.code).toBe(1);
    expect(result.stderr).toContain("404 not_found");
  });
});

describe("diagnose: plain output", () => {
  it("gives the value, the variation index and the reason", async () => {
    const result = await run([...BASE, "--context", '{"key":"u1"}']);
    expect(result.stdout).toBe(
      "value:           true\nvariationIndex:  0\nreason:          FALLTHROUGH\n",
    );
    expect(result.stderr).toBe("");
  });

  it("adds ruleIndex when the reason has one", async () => {
    const result = await run(
      [...BASE, "--context", '{"key":"u1"}'],
      reply({ kind: "RULE_MATCH", ruleIndex: 2 }, false, 1),
    );
    expect(result.stdout).toContain("reason:          RULE_MATCH\n");
    expect(result.stdout).toContain("ruleIndex:       2\n");
  });

  it("adds the error code, and exits 0 because the server answered 200", async () => {
    const result = await run(
      [...BASE, "--context", '{"key":"u1"}'],
      reply({ kind: "ERROR", code: "FLAG_NOT_FOUND" }, null, null),
    );
    expect(result.code).toBe(0);
    expect(result.stdout).toContain("value:           -\n");
    expect(result.stdout).toContain("variationIndex:  -\n");
    expect(result.stdout).toContain("reason:          ERROR\n");
    expect(result.stdout).toContain("code:            FLAG_NOT_FOUND\n");
  });

  it("names the prerequisite that failed", async () => {
    const result = await run(
      [...BASE, "--context", '{"key":"u1"}'],
      reply({ kind: "PREREQUISITE_FAILED", flagKey: "parent" }, false, 1),
    );
    expect(result.stdout).toContain("flagKey:         parent\n");
  });

  it("prints a string value as it is and any other value as JSON", async () => {
    const text = await run(
      [...BASE, "--context", '{"key":"u1"}'],
      reply({ kind: "FALLTHROUGH" }, "blue"),
    );
    expect(text.stdout).toContain("value:           blue\n");
    const object = await run(
      [...BASE, "--context", '{"key":"u1"}'],
      reply({ kind: "FALLTHROUGH" }, { a: [1] }),
    );
    expect(object.stdout).toContain('value:           {"a":[1]}\n');
  });

  it("--json prints the server's body unchanged, even when the reason is an error", async () => {
    const body = {
      flags: {
        "new-checkout": {
          value: null,
          variationIndex: null,
          reason: { kind: "ERROR", code: "FLAG_NOT_FOUND" },
        },
      },
    };
    const result = await run([...BASE, "--context", '{"key":"u1"}', "--json"], json(200, body));
    expect(result.code).toBe(0);
    expect(JSON.parse(result.stdout)).toEqual(body);
  });

  it("exits 1 when a 200 has no result for the flag", async () => {
    const result = await run([...BASE, "--context", '{"key":"u1"}'], json(200, { flags: {} }));
    expect(result.code).toBe(1);
    expect(result.stderr).toContain("without a result for new-checkout");
  });
});

/** A scratch directory holding a good context file and one that is not JSON. */
function scratch() {
  const dir = tempDir();
  const good = join(dir, "good.json");
  const bad = join(dir, "bad.json");
  writeFileSync(good, '{"key":"u1"}');
  writeFileSync(bad, "{not json");
  return { dir, good, bad, missing: join(dir, "nope.json") };
}

type Files = ReturnType<typeof scratch>;

describe("diagnose: exit 2, and no request is sent", () => {
  it.each<[string, (files: Files) => string[], string]>([
    [
      "no flag",
      () => ["diagnose", "-p", "shop", "-e", "production", "--context", "{}"],
      "the flag key",
    ],
    ["neither context option", () => BASE, "exactly one of"],
    [
      "both context options",
      (f) => [...BASE, "--context", "{}", "--context-file", f.good],
      "exactly one of",
    ],
    [
      "a context file that is missing",
      (f) => [...BASE, "--context-file", f.missing],
      "could not read",
    ],
    [
      "a context file that is a directory",
      (f) => [...BASE, "--context-file", f.dir],
      "could not read",
    ],
    [
      "a context file that is not JSON",
      (f) => [...BASE, "--context-file", f.bad],
      "not valid JSON",
    ],
    [
      "a context that is not JSON",
      () => [...BASE, "--context", "{key: u1}"],
      "--context is not valid JSON",
    ],
    [
      "no project",
      () => ["diagnose", "new-checkout", "-e", "production", "--context", "{}"],
      "--project",
    ],
    ["no env", () => ["diagnose", "new-checkout", "-p", "shop", "--context", "{}"], "--env"],
    ["two flags", () => [...BASE, "other", "--context", "{}"], "takes the flag key"],
  ])("%s", async (_name, args, message) => {
    const result = await run(args(scratch()));
    expect(result.code).toBe(2);
    expect(result.stdout).toBe("");
    expect(result.stderr).toContain(message);
    expect(result.seen).toEqual([]);
  });
});

describe("diagnose: exit 1", () => {
  it.each([
    [400, "bad_request", 'The context needs a non-empty string "key".'],
    [404, "not_found", "No such flag."],
    [404, "not_found", "No such environment."],
    [500, "internal", "boom"],
  ])("when the server answers %i %s", async (status, code, message) => {
    const result = await run(
      [...BASE, "--context", '{"key":"u1"}'],
      apiError(status, code, message),
    );
    expect(result.code).toBe(1);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe(`toggleslash: the server answered ${status} ${code}: ${message}\n`);
  });

  it("when the server cannot be reached", async () => {
    const result = await cli([...BASE, "--context", "{}"], {
      env: CONNECTED,
      fetch: () => Promise.reject(new Error("connect ECONNREFUSED 127.0.0.1:1")),
    });
    expect(result.code).toBe(1);
  });
});
