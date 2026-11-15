import { generateKeyPairSync, verify } from "node:crypto";
import { afterEach, describe, expect, it } from "vitest";

/**
 * The real entry point (`src/server.ts`) as a subprocess, against a fake ToggleSlash server on
 * loopback: the wiring is where a server key could leak, and where SIGTERM has to close the client.
 */
const SERVER_KEY = "tgl_server_a-key-that-must-stay-on-the-server";
const CLIENT_KEY = "tgl_client_a-key-for-browsers";

const ruleset = {
  flags: [
    {
      key: "new-checkout",
      variations: [true, false],
      on: true,
      offVariation: 1,
      targets: [],
      prerequisites: [],
      rules: [],
      fallthrough: { variation: 0 },
      salt: "s",
      version: 1,
    },
  ],
  segments: [],
};

interface Seen {
  path: string;
  authorization: string | null;
  body: unknown;
}

function fakeToggleSlash() {
  const seen: Seen[] = [];
  const server = Bun.serve({
    hostname: "127.0.0.1",
    port: 0,
    async fetch(request) {
      const path = new URL(request.url).pathname;
      let body: unknown = null;
      if (request.method === "POST") body = await request.json();
      seen.push({ path, authorization: request.headers.get("Authorization"), body });
      if (path === "/sdk/v1/ruleset") {
        return Response.json(ruleset, { headers: { ETag: '"1"' } });
      }
      // No stream: the SDK polls, which the test does not need to wait for.
      return path === "/sdk/v1/events"
        ? new Response(null, { status: 202 })
        : new Response(null, { status: 404 });
    },
  });
  return { seen, url: `http://127.0.0.1:${server.port}`, stop: () => server.stop(true) };
}

const cleanups: (() => unknown)[] = [];
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).toReversed()) await cleanup();
});

async function startExample(env: Record<string, string>) {
  const child = Bun.spawn(["bun", "src/server.ts"], {
    cwd: import.meta.dirname + "/..",
    env: { ...process.env, PORT: "0", ...env },
    stdin: "ignore",
    stdout: "pipe",
    stderr: "pipe",
  });
  cleanups.push(() => child.kill("SIGKILL"));
  const reader = child.stdout.getReader();
  const first = await reader.read();
  const match = /http:\/\/127\.0\.0\.1:\d+/.exec(new TextDecoder().decode(first.value));
  if (match === null)
    throw new Error(
      `the example did not say where it listens: ${await new Response(child.stderr).text()}`,
    );
  return { child, url: match[0] };
}

describe("src/server.ts", () => {
  it("never puts the server key in the page or /state, and closes the client on SIGTERM", async () => {
    const fake = fakeToggleSlash();
    cleanups.push(fake.stop);
    const { child, url } = await startExample({
      TOGGLESLASH_URL: fake.url,
      TOGGLESLASH_SDK_KEY: SERVER_KEY,
      TOGGLESLASH_CLIENT_KEY: CLIENT_KEY,
      TOGGLESLASH_FLUSH_INTERVAL_MS: "600000",
    });

    // The SDK loads the ruleset (the server key is in use) before the state shows the flag.
    let state: {
      flags: { key: string; value: unknown }[];
      status: { etag: string | null };
    } | null = null;
    for (let i = 0; i < 200 && state?.status.etag !== '"1"'; i++) {
      state = JSON.parse(await (await fetch(`${url}/state?user=free-user`)).text());
      await Bun.sleep(25);
    }
    expect(state?.status.etag).toBe('"1"');
    expect(state?.flags.find((f) => f.key === "new-checkout")?.value).toBe(true);
    expect(fake.seen.some((s) => s.authorization === `Bearer ${SERVER_KEY}`)).toBe(true);

    const page = await (await fetch(`${url}/?user=pro-user`)).text();
    const json = JSON.stringify(state);
    for (const body of [page, json]) {
      expect(body).not.toContain(SERVER_KEY);
      expect(body).not.toContain("tgl_server_");
    }
    expect(page).toContain(CLIENT_KEY);
    // The page loads the bundle from this server, and the bundle holds no key.
    expect(page).toContain('<script type="module" src="/browser.js"></script>');
    const bundle = await fetch(`${url}/browser.js`);
    expect(bundle.status).toBe(200);
    const bundleText = await bundle.text();
    expect(bundleText).not.toContain(SERVER_KEY);
    expect(bundleText).not.toContain("tgl_server_");

    // A day's counts are held for the next flush (ten minutes away) until the client closes.
    expect(fake.seen.filter((s) => s.path === "/sdk/v1/events")).toHaveLength(0);
    child.kill("SIGTERM");
    expect(await child.exited).toBe(0);
    expect(fake.seen.filter((s) => s.path === "/sdk/v1/events")).toHaveLength(1);
  });

  it("with TOGGLESLASH_SIGNING_KEY, signs the shopper's context and never puts the key in the page", async () => {
    const fake = fakeToggleSlash();
    cleanups.push(fake.stop);
    const pair = generateKeyPairSync("ec", { namedCurve: "P-256" });
    const pem = pair.privateKey.export({ type: "pkcs8", format: "pem" });
    const { url } = await startExample({
      TOGGLESLASH_URL: fake.url,
      TOGGLESLASH_SDK_KEY: SERVER_KEY,
      TOGGLESLASH_CLIENT_KEY: CLIENT_KEY,
      TOGGLESLASH_SIGNING_KEY: pem,
    });

    const page = await (await fetch(`${url}/?user=pro-user`)).text();
    const config = pageConfig(page);
    expect(config.user.key).toBe("pro-user");
    // The signed bytes for a user context are its kind and key as netstrings (docs/sdk-browser.md).
    const message = Buffer.from("4:user,8:pro-user,");
    const signature = Buffer.from(config.signature ?? "", "base64url");
    const options = { key: pair.publicKey, dsaEncoding: "ieee-p1363" } as const;
    expect(verify("sha256", message, options, signature)).toBe(true);
    expect(verify("sha256", Buffer.from("4:user,9:free-user,"), options, signature)).toBe(false);

    const bundle = await (await fetch(`${url}/browser.js`)).text();
    const state = await (await fetch(`${url}/state?user=pro-user`)).text();
    // Every line of the PEM's base64 body, as well as the armour: no piece of the key may leak.
    const pieces = pem.split("\n").filter((line) => line !== "");
    for (const body of [page, bundle, state]) {
      for (const piece of pieces) expect(body).not.toContain(piece);
      expect(body).not.toContain("PRIVATE KEY");
    }
  });

  it("without TOGGLESLASH_SIGNING_KEY, the page carries no signature", async () => {
    const fake = fakeToggleSlash();
    cleanups.push(fake.stop);
    const { url } = await startExample({
      TOGGLESLASH_URL: fake.url,
      TOGGLESLASH_SDK_KEY: SERVER_KEY,
      TOGGLESLASH_CLIENT_KEY: CLIENT_KEY,
    });
    const config = pageConfig(await (await fetch(`${url}/?user=pro-user`)).text());
    expect(config.user.key).toBe("pro-user");
    expect("signature" in config).toBe(false);
  });

  it("a TOGGLESLASH_SIGNING_KEY that cannot sign stops startup with exit 2", async () => {
    const child = Bun.spawn(["bun", "src/server.ts"], {
      cwd: import.meta.dirname + "/..",
      env: {
        ...process.env,
        PORT: "0",
        TOGGLESLASH_URL: "http://127.0.0.1:9",
        TOGGLESLASH_SDK_KEY: SERVER_KEY,
        TOGGLESLASH_CLIENT_KEY: CLIENT_KEY,
        TOGGLESLASH_SIGNING_KEY: "not a pem",
      },
      stdin: "ignore",
      stdout: "pipe",
      stderr: "pipe",
    });
    cleanups.push(() => child.kill("SIGKILL"));
    expect(await child.exited).toBe(2);
    expect(await new Response(child.stderr).text()).toContain(
      "TOGGLESLASH_SIGNING_KEY cannot sign",
    );
  });
});

/** The page's `#config` JSON. */
function pageConfig(page: string): { user: { key: string }; signature?: string } {
  const match = /<script type="application\/json" id="config">([^<]*)<\/script>/.exec(page);
  if (match === null) throw new Error("the page has no #config element");
  return JSON.parse(match[1] ?? "");
}
