import type { ClientStatus, DetailReason } from "@hackcove/featureslash-sdk";
import { describe, expect, it } from "vitest";
import { createApp, type FlagClient } from "../src/app.ts";
import { FLAGS } from "../src/flags.ts";
import { USERS } from "../src/users.ts";

const CLIENT_KEY = "fs_client_this-may-reach-a-browser";
const SERVER_URL = "http://127.0.0.1:9";

const STATUS: ClientStatus = {
  etag: '"7"',
  lastSyncAt: 1_700_000_000_000,
  stream: "connected",
  lastError: null,
};

/** A client that answers from a table and records which contexts it was asked about. */
function fakeClient(values: Record<string, unknown> = {}) {
  const asked: { flag: string; key: string; attributes: unknown }[] = [];
  const client: FlagClient = {
    variationDetail(flag, context, fallback) {
      asked.push({ flag, key: context.key, attributes: context.attributes });
      const reason: DetailReason = { kind: "FALLTHROUGH" };
      const value = flag in values ? values[flag] : fallback;
      return { value, variationIndex: 0, reason };
    },
    status: () => STATUS,
  };
  return { client, asked };
}

function app(client: FlagClient) {
  return createApp({ client, serverUrl: SERVER_URL, clientKey: CLIENT_KEY });
}

const get = (handler: ReturnType<typeof app>, path: string) =>
  handler(new Request(`http://example.test${path}`));

describe("GET /state", () => {
  it("answers every seeded flag's value and reason, and the SDK's status", async () => {
    const { client } = fakeClient({ "banner-text": "Hello" });
    const response = get(app(client), "/state?user=pro-user");
    expect(response.status).toBe(200);
    expect(response.headers.get("Cache-Control")).toBe("no-store");
    const state = JSON.parse(await response.text());

    expect(state.user).toEqual({ key: "pro-user", attributes: { plan: "pro", country: "NL" } });
    expect(state.flags.map((f: { key: string }) => f.key)).toEqual(FLAGS.map((f) => f.key));
    expect(state.flags[1]).toEqual({
      key: "banner-text",
      value: "Hello",
      variationIndex: 0,
      reason: { kind: "FALLTHROUGH" },
    });
    expect(state.status).toEqual(STATUS);
  });

  it("evaluates each flag for the chosen user's context, with its attributes", async () => {
    const { client, asked } = fakeClient();
    get(app(client), "/state?user=beta-user");
    expect(asked).toHaveLength(FLAGS.length);
    for (const call of asked) {
      expect(call.key).toBe("beta-user");
      expect(call.attributes).toMatchObject({ plan: "free", beta: true });
    }
  });

  it("serves the first user when none is named and refuses one it does not know", async () => {
    const { client } = fakeClient();
    const named = JSON.parse(await get(app(client), "/state").text());
    expect(named.user.key).toBe("free-user");

    const unknown = get(app(client), "/state?user=nobody");
    expect(unknown.status).toBe(400);
  });

  it("reads one more flag when asked with ?flag=, once, and refuses what is not a key", async () => {
    const { client, asked } = fakeClient({ "journey-flag": true });
    const handler = app(client);
    const state = JSON.parse(await get(handler, "/state?flag=journey-flag").text());
    expect(state.flags.map((f: { key: string }) => f.key)).toEqual([
      ...FLAGS.map((f) => f.key),
      "journey-flag",
    ]);
    expect(state.flags.at(-1)).toMatchObject({ key: "journey-flag", value: true });

    asked.length = 0;
    await get(handler, "/state?flag=new-checkout").text();
    expect(asked).toHaveLength(FLAGS.length); // a flag the shop already reads is not read twice

    for (const bad of ["Not A Key", "a/b", "x".repeat(65), ""]) {
      expect(get(handler, `/state?flag=${encodeURIComponent(bad)}`).status).toBe(400);
    }
  });

  it("only answers GET, and only its two routes", () => {
    const handler = app(fakeClient().client);
    expect(handler(new Request("http://example.test/state", { method: "POST" })).status).toBe(405);
    expect(get(handler, "/elsewhere").status).toBe(404);
  });
});

describe("GET /", () => {
  it("offers every user in the picker, with the current one selected", async () => {
    const html = await get(app(fakeClient().client), "/?user=pro-user").text();
    for (const user of USERS) expect(html).toContain(`<option value="${user.key}"`);
    expect(html).toContain('<option value="pro-user" selected>');
    expect(html).not.toContain('<option value="free-user" selected>');
    expect(html).toContain('data-testid="current-user">pro-user<');
  });

  it("has a stable test id on every value, status field and the button", async () => {
    const html = await get(app(fakeClient().client), "/").text();
    for (const flag of FLAGS) {
      expect(html).toContain(`data-testid="flag-${flag.key}-value"`);
      expect(html).toContain(`data-testid="flag-${flag.key}-reason"`);
    }
    for (const id of ["etag", "last-sync-at", "stream", "last-error"]) {
      expect(html).toContain(`data-testid="status-${id}"`);
    }
    expect(html).toContain('data-testid="evaluate-button"');
    expect(html).toContain(
      'data-testid="status-stream" data-path="status.stream" data-format="plain">connected<',
    );
  });

  it("keeps ?flag= across the picker and the page's refresh", async () => {
    const html = await get(app(fakeClient().client), "/?flag=journey-flag").text();
    expect(html).toContain('<input type="hidden" name="flag" value="journey-flag">');
    expect(html).toContain('data-testid="flag-journey-flag-value"');
    expect(html).toContain('"flag":"journey-flag"');
    expect(await get(app(fakeClient().client), "/").text()).not.toContain('name="flag"');
  });

  it("escapes what a flag serves", async () => {
    const { client } = fakeClient({ "banner-text": "<script>alert(1)</script>" });
    const html = await get(app(client), "/").text();
    expect(html).not.toContain("<script>alert(1)");
    expect(html).toContain("&lt;script&gt;");
  });
});
