import { describe, expect, it } from "vitest";
import { apiError, fakeServer, json } from "./fake-server.ts";
import { CONNECTED, TOKEN, cli } from "./harness.ts";

/** The token must not reach stdout or stderr by any path, even when the server (or a proxy) echoes it back. */
describe("the token never appears in output or errors", () => {
  it("is replaced when a server error message echoes it", async () => {
    const server = fakeServer({
      "GET /api/v1/projects": apiError(400, "bad_request", `bad header Bearer ${TOKEN}`),
    });
    const result = await cli(["projects", "list"], { fetch: server.fetch, env: CONNECTED });
    expect(result.code).toBe(1);
    expect(result.stderr).toContain("Bearer ***");
    expect(result.stdout + result.stderr).not.toContain(TOKEN);
  });

  it("is replaced when a network error message echoes it", async () => {
    const result = await cli(["projects", "list"], {
      env: CONNECTED,
      fetch: () => Promise.reject(new Error(`proxy refused Authorization: Bearer ${TOKEN}`)),
    });
    expect(result.code).toBe(1);
    expect(result.stdout + result.stderr).not.toContain(TOKEN);
  });

  it("is replaced in a successful reply that echoes it, human and --json", async () => {
    const server = fakeServer({
      "GET /api/v1/projects": json(200, [{ key: "k", name: TOKEN, createdAt: "x" }]),
    });
    for (const extra of [[], ["--json"]]) {
      const result = await cli(["projects", "list", ...extra], {
        fetch: server.fetch,
        env: CONNECTED,
      });
      expect(result.code).toBe(0);
      expect(result.stdout).not.toContain(TOKEN);
    }
  });

  it("is not printed by a usage error that follows a read of the config", async () => {
    const result = await cli(["flags", "list"], { env: CONNECTED });
    expect(result.code).toBe(2);
    expect(result.stdout + result.stderr).not.toContain(TOKEN);
  });

  it("is not printed by --help", async () => {
    const result = await cli(["--help"], { env: CONNECTED });
    expect(result.stdout + result.stderr).not.toContain(TOKEN);
  });
});
