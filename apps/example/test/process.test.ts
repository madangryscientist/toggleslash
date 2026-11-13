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
});
