import { existsSync, readFileSync, readdirSync, statSync, writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { fakeServer, json } from "./fake-server.ts";
import { TOKEN, URL_BASE, cli, tempDir } from "./harness.ts";

const file = (home: string): string => join(home, "toggleslash", "config.json");

describe("config set-url", () => {
  it("saves a normalised URL and prints where", async () => {
    const home = tempDir();
    const result = await cli(["config", "set-url", `${URL_BASE}/base///`], { configHome: home });
    expect(result.code).toBe(0);
    expect(JSON.parse(readFileSync(file(home), "utf8"))).toEqual({ url: `${URL_BASE}/base` });
    expect(result.stdout).toContain(file(home));
  });

  it.each([
    ["not a url", "is not a URL"],
    ["ftp://flags.example.test", "must start with http:// or https://"],
    ["https://user:pw@flags.example.test", "must not carry a user name or password"],
    ["https://flags.example.test/?a=b", "must not have a query"],
  ])("rejects %s with exit 2 and writes nothing", async (url, message) => {
    const home = tempDir();
    const result = await cli(["config", "set-url", url], { configHome: home });
    expect(result.code).toBe(2);
    expect(result.stderr).toContain(message);
    expect(result.stderr).not.toContain("pw");
    expect(existsSync(file(home))).toBe(false);
  });
});

describe("config set-token", () => {
  it("writes the file with mode 600 in a directory of mode 700, from stdin", async () => {
    const home = tempDir();
    const result = await cli(["config", "set-token"], { configHome: home, stdin: `${TOKEN}\n` });
    expect(result.code).toBe(0);
    expect(statSync(file(home)).mode & 0o777).toBe(0o600);
    expect(statSync(join(home, "toggleslash")).mode & 0o777).toBe(0o700);
    expect(JSON.parse(readFileSync(file(home), "utf8"))).toEqual({ token: TOKEN });
    expect(readdirSync(join(home, "toggleslash"))).toEqual(["config.json"]);
    expect(result.stdout + result.stderr).not.toContain(TOKEN);
  });

  it("refuses the token as an argument, so it cannot reach shell history", async () => {
    const home = tempDir();
    const result = await cli(["config", "set-token", TOKEN], { configHome: home });
    expect(result.code).toBe(2);
    expect(result.stderr).toContain("takes no arguments");
    expect(result.stderr).not.toContain(TOKEN);
    expect(existsSync(file(home))).toBe(false);
  });

  it("tightens a file that already exists with looser permissions", async () => {
    const home = tempDir();
    mkdirSync(join(home, "toggleslash"), { recursive: true });
    writeFileSync(file(home), "{}", { mode: 0o644 });
    await cli(["config", "set-token"], { configHome: home, stdin: TOKEN });
    expect(statSync(file(home)).mode & 0o777).toBe(0o600);
  });

  it("keeps the URL already saved", async () => {
    const home = tempDir();
    await cli(["config", "set-url", URL_BASE], { configHome: home });
    await cli(["config", "set-token"], { configHome: home, stdin: TOKEN });
    expect(JSON.parse(readFileSync(file(home), "utf8"))).toEqual({ url: URL_BASE, token: TOKEN });
  });

  it("exits 2 when stdin is a terminal and no argument was given, or the token is empty", async () => {
    const home = tempDir();
    expect((await cli(["config", "set-token"], { configHome: home, stdin: null })).code).toBe(2);
    expect((await cli(["config", "set-token"], { configHome: home, stdin: "  \n" })).code).toBe(2);
    expect(existsSync(file(home))).toBe(false);
  });

  it("falls back to ~/.config when XDG_CONFIG_HOME is unset", async () => {
    const home = tempDir();
    const outputs: string[] = [];
    const { run } = await import("../src/cli.ts");
    const code = await run(["config", "set-token"], {
      fetch: () => {
        throw new Error("no network");
      },
      env: {},
      home,
      readStdin: async () => TOKEN,
      stdout: (t) => void outputs.push(t),
      stderr: (t) => void outputs.push(t),
    });
    expect(code).toBe(0);
    expect(existsSync(join(home, ".config", "toggleslash", "config.json"))).toBe(true);
  });
});

function server(): ReturnType<typeof fakeServer> {
  return fakeServer({ "GET /api/v1/projects": json(200, []) });
}

describe("using the saved config", () => {
  it("calls the saved URL with the saved token", async () => {
    const home = tempDir();
    await cli(["config", "set-url", URL_BASE], { configHome: home });
    await cli(["config", "set-token"], { configHome: home, stdin: TOKEN });
    const fake = server();
    const result = await cli(["projects", "list"], { configHome: home, fetch: fake.fetch });
    expect(result.code).toBe(0);
    expect(fake.seen[0]?.headers["authorization"]).toBe(`Bearer ${TOKEN}`);
  });

  it("lets an environment variable win over the file, for each of the URL and the token", async () => {
    const home = tempDir();
    await cli(["config", "set-url", "https://from-file.test"], { configHome: home });
    await cli(["config", "set-token"], { configHome: home, stdin: "file-token-value" });
    const urls: string[] = [];
    const tokens: (string | null)[] = [];
    const fetch = async (input: string, init: RequestInit): Promise<Response> => {
      urls.push(input);
      tokens.push(new Headers(init.headers).get("authorization"));
      return json(200, []);
    };
    await cli(["projects", "list"], { configHome: home, fetch });
    await cli(["projects", "list"], {
      configHome: home,
      fetch,
      env: { TOGGLESLASH_URL: URL_BASE },
    });
    await cli(["projects", "list"], {
      configHome: home,
      fetch,
      env: { TOGGLESLASH_TOKEN: TOKEN },
    });
    expect(urls).toEqual([
      "https://from-file.test/api/v1/projects",
      `${URL_BASE}/api/v1/projects`,
      "https://from-file.test/api/v1/projects",
    ]);
    expect(tokens).toEqual([
      "Bearer file-token-value",
      "Bearer file-token-value",
      `Bearer ${TOKEN}`,
    ]);
  });

  it("treats an empty variable as unset", async () => {
    const home = tempDir();
    await cli(["config", "set-url", URL_BASE], { configHome: home });
    await cli(["config", "set-token"], { configHome: home, stdin: TOKEN });
    const fake = server();
    const result = await cli(["projects", "list"], {
      configHome: home,
      fetch: fake.fetch,
      env: { TOGGLESLASH_URL: "", TOGGLESLASH_TOKEN: "  " },
    });
    expect(result.code).toBe(0);
    expect(fake.seen[0]?.headers["authorization"]).toBe(`Bearer ${TOKEN}`);
  });

  it("exits 2 naming what is missing", async () => {
    const urlOnly = await cli(["projects", "list"], { env: { TOGGLESLASH_URL: URL_BASE } });
    expect(urlOnly.code).toBe(2);
    expect(urlOnly.stderr).toContain("no admin token");
    const tokenOnly = await cli(["projects", "list"], { env: { TOGGLESLASH_TOKEN: TOKEN } });
    expect(tokenOnly.code).toBe(2);
    expect(tokenOnly.stderr).toContain("no server URL");
    expect(tokenOnly.stderr).not.toContain(TOKEN);
  });

  it("exits 2 for a config file that is not JSON, without printing its contents", async () => {
    const home = tempDir();
    mkdirSync(join(home, "toggleslash"), { recursive: true });
    writeFileSync(file(home), `token=${TOKEN}`);
    const result = await cli(["projects", "list"], { configHome: home });
    expect(result.code).toBe(2);
    expect(result.stderr).toContain("is not valid JSON");
    expect(result.stderr).not.toContain(TOKEN);
  });

  it("does not read the file when the environment has both settings", async () => {
    const home = tempDir();
    mkdirSync(join(home, "toggleslash"), { recursive: true });
    writeFileSync(file(home), "not json");
    const fake = server();
    const result = await cli(["projects", "list"], {
      configHome: home,
      fetch: fake.fetch,
      env: { TOGGLESLASH_URL: URL_BASE, TOGGLESLASH_TOKEN: TOKEN },
    });
    expect(result.code).toBe(0);
  });
});
