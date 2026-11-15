import { describe, expect, it } from "vitest";
import { P } from "./case.ts";
import { fakeServer, json } from "./fake-server.ts";
import { CONNECTED, cli } from "./harness.ts";

const PEM = "-----BEGIN PRIVATE KEY-----\nPLAINPRIVATEKEY\n-----END PRIVATE KEY-----";
const SIGNING_KEY = `POST ${P}/shop/environments/prod/signing-key`;
const ENVIRONMENT = `PATCH ${P}/shop/environments/prod`;
const env = {
  key: "prod",
  name: "Prod",
  createdAt: "2026-11-01T10:05:00Z",
  signingKeyCreatedAt: null,
};

describe("envs signing-key", () => {
  it("prints the PEM and nothing else to stdout, and the reminder to stderr", async () => {
    const server = fakeServer({
      [SIGNING_KEY]: json(201, { privateKey: PEM, createdAt: "2026-11-15T08:00:00Z" }),
    });
    const result = await cli(["envs", "signing-key", "prod", "-p", "shop"], {
      fetch: server.fetch,
      env: CONNECTED,
    });
    expect(result.code).toBe(0);
    expect(result.stdout).toBe(`${PEM}\n`);
    expect(result.stderr).toContain("will not show it again");
    expect(result.stderr).not.toContain("PLAINPRIVATEKEY");
    expect(server.seen[0]?.body).toBeUndefined();
  });

  it("still reminds on stderr with --json", async () => {
    const server = fakeServer({
      [SIGNING_KEY]: json(201, { privateKey: PEM, createdAt: "2026-11-15T08:00:00Z" }),
    });
    const result = await cli(["envs", "signing-key", "prod", "-p", "shop", "--json"], {
      fetch: server.fetch,
      env: CONNECTED,
    });
    expect(JSON.parse(result.stdout)).toEqual({
      privateKey: PEM,
      createdAt: "2026-11-15T08:00:00Z",
    });
    expect(result.stderr).toContain("will not show it again");
  });

  it("exits 1 when the answer carries no private key", async () => {
    const server = fakeServer({ [SIGNING_KEY]: json(201, { createdAt: "2026-11-15T08:00:00Z" }) });
    const result = await cli(["envs", "signing-key", "prod", "-p", "shop"], {
      fetch: server.fetch,
      env: CONNECTED,
    });
    expect(result.code).toBe(1);
    expect(result.stdout).toBe("");
  });
});

describe("envs secure-mode", () => {
  it("sends false for off", async () => {
    const server = fakeServer({ [ENVIRONMENT]: json(200, { ...env, secureMode: false }) });
    const result = await cli(["envs", "secure-mode", "prod", "off", "-p", "shop"], {
      fetch: server.fetch,
      env: CONNECTED,
    });
    expect(result.code).toBe(0);
    expect(server.seen[0]?.body).toEqual({ secureMode: false });
    expect(result.stdout).toBe("secure mode is off for environment prod in shop\n");
  });

  it("exits 2 on anything but on or off, and sends nothing", async () => {
    for (const args of [["yes"], [], ["on", "extra"]]) {
      const server = fakeServer({});
      const result = await cli(["envs", "secure-mode", "prod", ...args, "-p", "shop"], {
        fetch: server.fetch,
        env: CONNECTED,
      });
      expect(result.code).toBe(2);
      expect(server.seen).toHaveLength(0);
    }
  });
});
