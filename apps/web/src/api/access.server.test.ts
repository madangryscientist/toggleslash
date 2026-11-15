// The connection's environment and Cloudflare Access: what `readConnection` accepts, the service token's
// headers, and how a refusal reads behind Access.
import { describe, expect, test } from "vitest";
import { ApiFailure, createAdminApi, readConnection, type Fetch } from "./client.server";

const TOKEN = "unit-test-token-123";
const ACCESS_ID = "unit-test-access-id.access";
const ACCESS_SECRET = "unit-test-access-secret-456";
const plain = { url: "http://api.test", token: TOKEN };
const behindAccess = { ...plain, access: { clientId: ACCESS_ID, clientSecret: ACCESS_SECRET } };

/** Answers every call with `response` and keeps the headers each call sent. */
function fakeFetch(response: () => Response) {
  const sent: Headers[] = [];
  const doFetch: Fetch = (_input, init) => {
    sent.push(new Headers(init.headers));
    return Promise.resolve(response());
  };
  return { doFetch, sent };
}

async function failureOf(on: typeof plain, response: () => Response): Promise<ApiFailure> {
  const error = await createAdminApi(on, fakeFetch(response).doFetch)
    .projects()
    .then(
      () => undefined,
      (e: unknown) => e,
    );
  if (!(error instanceof ApiFailure)) throw new Error("expected an ApiFailure");
  return error;
}

describe("the Access headers", () => {
  test("both go out beside the bearer when configured", async () => {
    const { doFetch, sent } = fakeFetch(() => Response.json([]));
    await createAdminApi(behindAccess, doFetch).projects();
    expect(sent[0]?.get("authorization")).toBe(`Bearer ${TOKEN}`);
    expect(sent[0]?.get("cf-access-client-id")).toBe(ACCESS_ID);
    expect(sent[0]?.get("cf-access-client-secret")).toBe(ACCESS_SECRET);
  });

  test("neither goes out when not configured", async () => {
    const { doFetch, sent } = fakeFetch(() => Response.json([]));
    await createAdminApi(plain, doFetch).projects();
    expect(sent[0]?.get("authorization")).toBe(`Bearer ${TOKEN}`);
    expect(sent[0]?.has("cf-access-client-id")).toBe(false);
    expect(sent[0]?.has("cf-access-client-secret")).toBe(false);
  });
});

describe("a refusal", () => {
  test("behind Access, a 403 does not blame the admin token alone", async () => {
    const error = await failureOf(behindAccess, () => new Response("Forbidden", { status: 403 }));
    expect(error.kind).toBe("unauthorized");
    expect(error.message).toBe("The admin API refused the request (403).");
  });

  test("a 3xx (Access's login redirect) is unauthorized and says so", async () => {
    const error = await failureOf(
      behindAccess,
      () =>
        new Response(null, {
          status: 302,
          headers: { location: "https://team.cloudflareaccess.com/cdn-cgi/access/login" },
        }),
    );
    expect(error.kind).toBe("unauthorized");
    expect(error.status).toBe(302);
    expect(error.message).toContain("redirected");
    expect(error.message).toContain("Cloudflare Access");
  });
});

describe("readConnection", () => {
  const base = { url: "http://localhost:5000/", token: "t" };

  test("accepts an http(s) url and a token, and drops a trailing slash", () => {
    expect(readConnection({ ...base, accessClientId: undefined, accessClientSecret: "" })).toEqual({
      ok: true,
      connection: { url: "http://localhost:5000", token: "t" },
    });
  });

  test("with both Access values, the connection carries the service token", () => {
    expect(
      readConnection({ ...base, accessClientId: "id.access", accessClientSecret: "s" }),
    ).toEqual({
      ok: true,
      connection: {
        url: "http://localhost:5000",
        token: "t",
        access: { clientId: "id.access", clientSecret: "s" },
      },
    });
  });

  test.each([
    ["the id alone", { accessClientId: "id-value", accessClientSecret: "" }],
    ["the secret alone", { accessClientId: undefined, accessClientSecret: "secret-value" }],
  ])("refuses %s, naming both variables and neither value", (_, access) => {
    const result = readConnection({ ...base, ...access });
    expect(result.ok).toBe(false);
    const text = JSON.stringify(result);
    expect(text).toContain("TOGGLESLASH_ACCESS_CLIENT_ID");
    expect(text).toContain("TOGGLESLASH_ACCESS_CLIENT_SECRET");
    expect(text).not.toContain("id-value");
    expect(text).not.toContain("secret-value");
  });

  test.each([
    [undefined, undefined],
    ["http://x", undefined],
    ["ftp://x", "secret-value"],
    ["not a url", "secret-value"],
  ])("says which variable is missing or wrong, never a value (%s, %s)", (url, token) => {
    const result = readConnection({
      url,
      token,
      accessClientId: undefined,
      accessClientSecret: undefined,
    });
    expect(result.ok).toBe(false);
    expect(JSON.stringify(result)).toMatch(/TOGGLESLASH_/);
    expect(JSON.stringify(result)).not.toContain("secret-value");
  });
});
