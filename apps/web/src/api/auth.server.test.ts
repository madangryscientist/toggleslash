import { describe, expect, test } from "vitest";
import {
  clearedSessionCookie,
  configProblems,
  gate,
  hasSession,
  passwordMatches,
  SESSION_COOKIE,
  SESSION_SECONDS,
  sessionCookie,
  signSession,
  verifySession,
} from "./auth.server.ts";

const NOW = 1_800_000_000_000;
const SECRET = "s".repeat(32);
const PASSWORD = "correct horse battery";
const secrets = { password: PASSWORD, sessionSecret: SECRET };

function requestWith(cookie: string | undefined, path = "/keys"): Request {
  return new Request(`https://dash.test${path}`, {
    headers: cookie === undefined ? {} : { cookie },
  });
}

describe("the session cookie", () => {
  test("a signed value verifies until it expires 12 hours ahead", async () => {
    const value = await signSession(SECRET, NOW);
    expect(await verifySession(SECRET, value, NOW)).toBe(true);
    expect(await verifySession(SECRET, value, NOW + SESSION_SECONDS * 1000 - 1)).toBe(true);
    expect(SESSION_SECONDS).toBe(12 * 60 * 60);
  });

  test("an expired one is refused", async () => {
    const value = await signSession(SECRET, NOW);
    expect(await verifySession(SECRET, value, NOW + SESSION_SECONDS * 1000)).toBe(false);
  });

  test("one signed with another secret is refused", async () => {
    const value = await signSession("o".repeat(32), NOW);
    expect(await verifySession(SECRET, value, NOW)).toBe(false);
  });

  test("changing any character of it is refused", async () => {
    const value = await signSession(SECRET, NOW);
    for (let i = 0; i < value.length; i += 1) {
      const swapped = value[i] === "0" ? "1" : "0";
      const tampered = `${value.slice(0, i)}${swapped}${value.slice(i + 1)}`;
      expect(await verifySession(SECRET, tampered, NOW)).toBe(false);
    }
  });

  test.each([
    undefined,
    "",
    "garbage",
    ".",
    "1.",
    `.${"a".repeat(64)}`,
    `abc.${"a".repeat(64)}`,
    `1.${"A".repeat(64)}`,
  ])("%s is refused", async (value) => {
    expect(await verifySession(SECRET, value, NOW)).toBe(false);
  });

  test("the attributes: HttpOnly, Lax, root path, Secure except on loopback", () => {
    const remote = sessionCookie("https://dash.test/sign-in", "v");
    expect(remote).toBe(
      `${SESSION_COOKIE}=v; Path=/; HttpOnly; SameSite=Lax; Max-Age=43200; Secure`,
    );
    for (const host of ["localhost", "127.0.0.1", "[::1]"]) {
      expect(sessionCookie(`http://${host}:4420/`, "v")).not.toContain("Secure");
    }
    // A name that only starts like a loopback host is not one.
    expect(sessionCookie("http://localhost.evil.test/", "v")).toContain("Secure");
    expect(clearedSessionCookie("https://dash.test/")).toContain("Max-Age=0");
  });
});

describe("the password check", () => {
  test("accepts the password and nothing else", async () => {
    expect(await passwordMatches(secrets, PASSWORD)).toBe(true);
    for (const wrong of ["", "correct horse", `${PASSWORD} `, PASSWORD.toUpperCase()]) {
      expect(await passwordMatches(secrets, wrong)).toBe(false);
    }
  });
});

describe("failing closed", () => {
  const value = async () => `${SESSION_COOKIE}=${await signSession(SECRET, NOW)}`;

  test.each([
    ["both unset", {}, ["TOGGLESLASH_OPERATOR_PASSWORD", "TOGGLESLASH_SESSION_SECRET"]],
    ["no password", { sessionSecret: SECRET }, ["TOGGLESLASH_OPERATOR_PASSWORD"]],
    ["no session secret", { password: PASSWORD }, ["TOGGLESLASH_SESSION_SECRET"]],
    [
      "a password of 15 characters",
      { password: "p".repeat(15), sessionSecret: SECRET },
      ["TOGGLESLASH_OPERATOR_PASSWORD"],
    ],
    [
      "a session secret of 31 characters",
      { password: PASSWORD, sessionSecret: "s".repeat(31) },
      ["TOGGLESLASH_SESSION_SECRET"],
    ],
  ])("%s names the variables and lets nobody in", async (_name, partial, variables) => {
    const config = { password: undefined, sessionSecret: undefined, ...partial };
    expect(configProblems(config).map((p) => p.variable)).toEqual(variables);
    expect(await hasSession(requestWith(await value()), config, NOW)).toBe(false);

    const signIn = await gate(
      new Request("https://dash.test/sign-in", {
        method: "POST",
        body: new URLSearchParams({ password: config.password ?? "" }),
      }),
      config,
      NOW,
      () => new Response("rendered"),
    );
    expect(signIn.headers.get("set-cookie")).toBeNull();
    expect(signIn.headers.get("location")).toContain("failed=true");
  });

  test("the minimum lengths are enough", () => {
    expect(configProblems({ password: "p".repeat(16), sessionSecret: "s".repeat(32) })).toEqual([]);
  });
});

const rendered = () => new Response("rendered");
const signedInCookie = async () => `other=1; ${SESSION_COOKIE}=${await signSession(SECRET, NOW)}`;

function post(password: string, returnTo: string): Request {
  return new Request("https://dash.test/sign-in", {
    method: "POST",
    body: new URLSearchParams({ password, returnTo }),
  });
}

describe("the gate", () => {
  test("the sign-in page sends someone signed in on", async () => {
    const response = await gate(
      requestWith(await signedInCookie(), "/sign-in?returnTo=%2Fkeys"),
      secrets,
      NOW,
      rendered,
    );
    expect(response.headers.get("location")).toBe("/keys");
  });

  test("a form that cannot be read is a failure, not a crash", async () => {
    const response = await gate(
      new Request("https://dash.test/sign-in", {
        method: "POST",
        headers: { "content-type": "multipart/form-data; boundary=x" },
        body: "not a form",
      }),
      secrets,
      NOW,
      rendered,
    );
    expect(response.headers.get("location")).toContain("failed=true");
  });

  test("signing in never leaves this origin", async () => {
    const response = await gate(post(PASSWORD, "https://evil.test"), secrets, NOW, rendered);
    expect(response.headers.get("location")).toBe("/");
  });
});
