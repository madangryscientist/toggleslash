import { describe, expect, it } from "vitest";
import { createNonce, withSecurityHeaders } from "./security-headers.server.ts";

describe("withSecurityHeaders", () => {
  it("puts the headers on a redirect, with its status and location kept", async () => {
    const response = await withSecurityHeaders("abc", () =>
      Response.redirect("https://dash.example/sign-in", 303),
    );
    expect(response.status).toBe(303);
    expect(response.headers.get("location")).toBe("https://dash.example/sign-in");
    expect(response.headers.get("x-content-type-options")).toBe("nosniff");
    expect(response.headers.get("referrer-policy")).toBe("no-referrer");
    expect(response.headers.get("x-frame-options")).toBe("DENY");
    expect(response.headers.get("cache-control")).toBe("no-store");
  });

  it("keeps the body, the other headers and every cookie", async () => {
    const inner = new Response("hello", { status: 401, headers: { "x-own": "1" } });
    inner.headers.append("set-cookie", "a=1");
    inner.headers.append("set-cookie", "b=2");
    const response = await withSecurityHeaders("abc", () => inner);
    expect(response.status).toBe(401);
    expect(await response.text()).toBe("hello");
    expect(response.headers.get("x-own")).toBe("1");
    expect(response.headers.getSetCookie()).toEqual(["a=1", "b=2"]);
  });

  it("overrides a weaker header the handler set, and names the nonce in the policy", async () => {
    const response = await withSecurityHeaders(
      "abc",
      () =>
        new Response("", {
          headers: { "x-frame-options": "SAMEORIGIN", "cache-control": "public" },
        }),
    );
    expect(response.headers.get("x-frame-options")).toBe("DENY");
    expect(response.headers.get("cache-control")).toBe("no-store");
    expect(response.headers.get("content-security-policy")).toContain(
      "script-src 'self' 'nonce-abc'",
    );
  });
});

describe("createNonce", () => {
  it("differs between calls", () => {
    expect(createNonce()).not.toBe(createNonce());
  });
});
