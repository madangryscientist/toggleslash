import { writeFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { P } from "./case.ts";
import { edited } from "./targeting-cases.ts";
import { segment } from "./segment-cases.ts";
import { fakeServer, json } from "./fake-server.ts";
import { CONNECTED, cli, tempDir } from "./harness.ts";

const SET = ["targeting", "set", "new-checkout", "-p", "shop", "-e", "production"];
const SET_PATH = `${P}/shop/flags/new-checkout/environments/production`;
const SEGMENTS = `${P}/shop/environments/production/segments`;

function file(name: string, text: string): string {
  const path = join(tempDir(), name);
  writeFileSync(path, text);
  return path;
}

describe("targeting set --file", () => {
  it("sends the file's object as the body of a PUT, as parsed", async () => {
    const text = JSON.stringify({ ...edited, extra: { nested: [1, "a", null] } }, null, 4);
    const server = fakeServer({ [`PUT ${SET_PATH}`]: json(200, { ...edited, version: 5 }) });
    const result = await cli([...SET, "--file", file("t.json", text)], {
      fetch: server.fetch,
      env: CONNECTED,
    });
    expect(result.code).toBe(0);
    expect(result.stdout).toBe("saved new-checkout in production at version 5\n");
    expect(server.seen).toHaveLength(1);
    expect(server.seen[0]?.method).toBe("PUT");
    expect(server.seen[0]?.body).toEqual(JSON.parse(text));
  });

  it("round trips: what `targeting show` prints is a valid --file", async () => {
    const shown = await cli(
      ["targeting", "show", "new-checkout", "-p", "shop", "-e", "production"],
      { fetch: fakeServer({ [`GET ${SET_PATH}`]: json(200, edited) }).fetch, env: CONNECTED },
    );
    const server = fakeServer({ [`PUT ${SET_PATH}`]: json(200, edited) });
    await cli([...SET, "--file", file("t.json", shown.stdout)], {
      fetch: server.fetch,
      env: CONNECTED,
    });
    expect(server.seen[0]?.body).toEqual(edited);
  });

  it("--file - with no stdin (a terminal) exits 2 and sends nothing", async () => {
    const server = fakeServer({});
    const result = await cli([...SET, "--file", "-"], { fetch: server.fetch, env: CONNECTED });
    expect(result.code).toBe(2);
    expect(result.stderr).toContain("stdin is a terminal");
    expect(server.seen).toEqual([]);
  });

  it("requires --file", async () => {
    const result = await cli(SET, { env: CONNECTED });
    expect(result.code).toBe(2);
    expect(result.stderr).toBe("toggleslash: --file is required.\n");
  });

  it.each([
    ["a file that is not JSON", "{ nope", "is not valid JSON."],
    ["a file holding an array", "[1]", "must hold a JSON object."],
    ["a file holding null", "null", "must hold a JSON object."],
  ])("exits 2 for %s and makes no request", async (_name, text, message) => {
    const server = fakeServer({});
    const path = file("t.json", text);
    const result = await cli([...SET, "--file", path], { fetch: server.fetch, env: CONNECTED });
    expect(result.code).toBe(2);
    expect(result.stderr).toBe(`toggleslash: --file ${path} ${message}\n`);
    expect(server.seen).toEqual([]);
  });

  it("exits 2 for a missing file and makes no request", async () => {
    const server = fakeServer({});
    const path = join(tempDir(), "missing.json");
    const result = await cli([...SET, "--file", path], { fetch: server.fetch, env: CONNECTED });
    expect(result.code).toBe(2);
    expect(result.stderr).toContain(`toggleslash: could not read --file ${path}: ENOENT`);
    expect(server.seen).toEqual([]);
  });
});

describe("segments --file", () => {
  const create = ["segments", "create", "beta", "-p", "shop", "-e", "production"];
  const put = ["segments", "set", "beta", "-p", "shop", "-e", "production"];

  it("create sends a file's arrays, fills the ones it leaves out and uses the positional key", async () => {
    const server = fakeServer({ [`POST ${SEGMENTS}`]: json(201, segment) });
    const path = file("s.json", JSON.stringify({ key: "beta", included: ["u1"] }));
    const result = await cli([...create, "--file", path], { fetch: server.fetch, env: CONNECTED });
    expect(result.code).toBe(0);
    expect(server.seen[0]?.body).toEqual({
      key: "beta",
      included: ["u1"],
      excluded: [],
      rules: [],
    });
  });

  it.each([create, put])(
    "%s: a file whose key differs exits 2 and makes no request",
    async (...args) => {
      const server = fakeServer({});
      const path = file("s.json", JSON.stringify({ key: "other", included: [] }));
      const result = await cli([...args, "--file", path], { fetch: server.fetch, env: CONNECTED });
      expect(result.code).toBe(2);
      expect(result.stderr).toContain("other");
      expect(server.seen).toEqual([]);
    },
  );

  it("set drops an equal key, because the server refuses unknown fields", async () => {
    const server = fakeServer({ [`PUT ${SEGMENTS}/beta`]: json(200, segment) });
    const path = file("s.json", JSON.stringify(segment));
    await cli([...put, "--file", path], { fetch: server.fetch, env: CONNECTED });
    expect(server.seen[0]?.body).not.toHaveProperty("key");
    expect(server.seen[0]?.body).toHaveProperty("version", 3);
  });

  it("set requires --file", async () => {
    const result = await cli(put, { env: CONNECTED });
    expect(result.code).toBe(2);
  });
});
