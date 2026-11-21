import { signContext } from "@slashsuite/toggleslash-sdk";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { createCli, type Cli } from "../src/cli.ts";
import { record, text } from "../src/data.ts";
import { skipReason } from "../src/dotnet.ts";
import { buildServer, startServer, type RunningServer } from "../src/server.ts";

/**
 * Secure mode across languages: the real C# server, a signing key and secure mode set with the real CLI, and a
 * signature made by the TypeScript server SDK's `signContext` that the server's .NET ECDSA verifies.
 */
const skip = skipReason() !== null;

const ADMIN_TOKEN = "e2e-throwaway-admin-token-not-a-secret";
const PROJECT = "shop";
const ENV = "production";

const code = (body: Record<string, unknown>) => record(body["error"])["code"];

describe.skipIf(skip)("secure mode end to end", () => {
  let scratch = "";
  let server: RunningServer | null = null;
  let cli: Cli;
  let clientKey = "";
  let privateKey = "";

  const evaluate = async (body: unknown) => {
    const response = await fetch(`${server?.url}/sdk/v1/evaluate`, {
      method: "POST",
      headers: { Authorization: `Bearer ${clientKey}`, "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    return { status: response.status, body: record(await response.json()) };
  };

  beforeAll(async () => {
    scratch = mkdtempSync(join(tmpdir(), "toggleslash-e2e-secure-"));
    mkdirSync(join(scratch, "home"));
    const assembly = await buildServer();
    server = await startServer(assembly, {
      dbPath: join(scratch, "toggleslash.db"),
      adminToken: ADMIN_TOKEN,
    });
    cli = createCli({ url: server.url, token: ADMIN_TOKEN, home: join(scratch, "home") });

    await cli.json(["projects", "create", PROJECT]);
    await cli.json(["envs", "create", ENV, "--project", PROJECT]);
    await cli.json(["flags", "create", "new-checkout", "--project", PROJECT, "--client-visible"]);
    const created = record(
      await cli.json([
        "keys",
        "create",
        "--project",
        PROJECT,
        "--env",
        ENV,
        "--kind",
        "client",
        "--name",
        "page",
      ]),
    );
    clientKey = text(created["key"]);
  });

  afterAll(async () => {
    try {
      await server?.stop();
    } finally {
      rmSync(scratch, { recursive: true, force: true });
    }
  });

  it("makes a signing key and turns secure mode on with the CLI", async () => {
    const made = await cli.run(["envs", "signing-key", ENV, "--project", PROJECT]);
    expect(made.code).toBe(0);
    expect(made.stdout).toMatch(
      /^-----BEGIN PRIVATE KEY-----\n[\s\S]+\n-----END PRIVATE KEY-----\n$/,
    );
    expect(made.stderr).toContain("will not show it again");
    privateKey = made.stdout;

    const on = await cli.run(["envs", "secure-mode", ENV, "on", "--project", PROJECT]);
    expect(on.code).toBe(0);
    const listed = await cli.run(["envs", "list", "--project", PROJECT]);
    expect(listed.stdout).toMatch(/production\s+production\s+on/);
  });

  it("refuses a client-key evaluate with no signature", async () => {
    const unsigned = await evaluate({ context: { key: "user-1" } });
    expect(unsigned.status).toBe(403);
    expect(code(unsigned.body)).toBe("signature_required");
  });

  it("answers a context signed with signContext, single and multi", async () => {
    const single = { key: "user-1", attributes: { plan: "pro" } };
    const answered = await evaluate({
      context: single,
      signature: signContext(privateKey, single),
    });
    expect(answered.status).toBe(200);
    expect(Object.keys(record(answered.body["flags"]))).toEqual(["new-checkout"]);

    const multi = {
      kind: "multi" as const,
      contexts: { user: { key: "u" }, org: { key: "acme" } },
    };
    const both = await evaluate({ context: multi, signature: signContext(privateKey, multi) });
    expect(both.status).toBe(200);
  });
});
