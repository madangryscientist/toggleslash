import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { expect, test, type APIRequestContext, type Page } from "@playwright/test";
import {
  API_PORT,
  E2E_ADMIN_TOKEN,
  E2E_OPERATOR_PASSWORD,
  E2E_SESSION_SECRET,
  PORT,
  SIGNED_IN_STATE,
  UNCONFIGURED_PORT,
} from "./constants.ts";
import { gotoHydrated } from "./goto.ts";

/** A section link in the dashboard nav. The nav is also in a phone menu, hidden on desktop, so match by role. */
const section = (page: Page, label: string) =>
  page
    .getByRole("navigation", { name: "Sections" })
    .getByRole("link", { name: label, exact: true });

const API = `http://127.0.0.1:${API_PORT}`;
const UNCONFIGURED = `http://localhost:${UNCONFIGURED_PORT}`;
const ORIGIN = `http://localhost:${PORT}`;
const CLIENT_BUILD = join(import.meta.dirname, "../dist/client");
const COOKIE = "toggleslash_session";
const SIGNED_OUT = { cookies: [], origins: [] };

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

function filesUnder(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? filesUnder(path) : [path];
  });
}

/** Every server function in the production build, from the manifest the Worker looks them up in. */
function builtServerFunctions(): Map<string, string> {
  const functions = new Map<string, string>();
  const assets = join(import.meta.dirname, "../dist/server/assets");
  for (const file of readdirSync(assets).filter((f) => /server-fn-resolver.*\.js$/.test(f))) {
    const manifest = readFileSync(join(assets, file), "utf8");
    for (const match of manifest.matchAll(/"([0-9a-f]{64})":\s*\{\s*functionName: "(\w+?)_/g)) {
      functions.set(match[2] ?? "", `/_serverFn/${match[1] ?? ""}`);
    }
  }
  return functions;
}

/** Each server function the source exports, so a scan that misses one fails. */
function declaredServerFunctions(): string[] {
  return filesUnder(join(import.meta.dirname, "../src"))
    .filter((file) => /\.tsx?$/.test(file) && !file.includes(".test."))
    .flatMap((file) =>
      [...readFileSync(file, "utf8").matchAll(/export const (\w+) = createServerFn\(/g)].map(
        (match) => match[1] ?? "",
      ),
    );
}

/** By both methods, as a manifest does not say which is right: the middleware's 401, then the 405. */
async function expectRefused(request: APIRequestContext, base: string, name: string, url: string) {
  const responses = [await request.get(`${base}${url}`), await request.post(`${base}${url}`)];
  const statuses = responses.map((response) => response.status()).toSorted((a, b) => a - b);
  expect({ name, statuses }).toEqual({ name, statuses: [401, 405] });
}

/** The session cookie the setup project signed in with. */
function signedInCookie(): { name: string; value: string } {
  const state: { cookies: { name: string; value: string }[] } = JSON.parse(
    readFileSync(SIGNED_IN_STATE, "utf8"),
  );
  const cookie = state.cookies.find((c) => c.name === COOKIE);
  if (cookie === undefined) throw new Error("the setup project saved no session cookie");
  return cookie;
}

test.describe("signed out", () => {
  test.use({ storageState: SIGNED_OUT });

  test("a page redirects to sign-in, and the form returns there afterwards", async ({ page }) => {
    await gotoHydrated(page, "/keys?project=shop&env=staging");
    await expect(page).toHaveURL(/\/sign-in\?returnTo=%2Fkeys%3Fproject%3Dshop%26env%3Dstaging/);
    await page.getByLabel("Password").fill(E2E_OPERATOR_PASSWORD);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page).toHaveURL(/\/keys\?project=shop&env=staging$/);
    await expect(page.getByLabel("Environment")).toHaveValue("staging");
  });

  test("a wrong password sets no cookie, and the page it lands on says only that it failed", async ({
    request,
  }) => {
    const answer = await request.post("/sign-in", {
      form: { password: "not the password at all", returnTo: "/" },
      maxRedirects: 0,
    });
    expect(answer.status()).toBe(303);
    expect(answer.headers()["set-cookie"]).toBeUndefined();
    const location = answer.headers()["location"] ?? "";
    expect(location).toMatch(/\/sign-in\?.*failed=true/);
    expect(location).not.toContain("password");

    const page = await request.get(location);
    const html = await page.text();
    expect(html).toContain("That did not work");
    expect(html).toContain("Check the password and try again.");
  });

  test("direct requests to every server function return no data", async ({ request }) => {
    const functions = builtServerFunctions();
    expect([...functions.keys()].toSorted()).toEqual(declaredServerFunctions().toSorted());
    for (const [name, url] of functions) {
      await expectRefused(request, ORIGIN, name, url);
    }
  });

  test("a tampered cookie is refused by pages and by server functions", async ({ request }) => {
    const real = signedInCookie();
    const [expiry, signature = ""] = real.value.split(".");
    const flipped = `${signature.startsWith("0") ? "1" : "0"}${signature.slice(1)}`;
    const cookie = `${COOKIE}=${expiry ?? ""}.${flipped}`;

    const page = await request.get("/", { headers: { cookie }, maxRedirects: 0 });
    expect(page.status()).toBe(303);
    expect(page.headers()["location"]).toMatch(/\/sign-in/);
    const url = builtServerFunctions().get("loadEnvironments") ?? "";
    const call = await request.get(url, { headers: { cookie, "x-tsr-serverfn": "true" } });
    expect(call.status()).toBe(401);
  });

  test("with neither secret set, the sign-in page names the variables and nobody gets in", async ({
    request,
  }) => {
    const page = await request.get(`${UNCONFIGURED}/`);
    expect(new URL(page.url()).pathname).toBe("/sign-in");
    const html = await page.text();
    expect(html).toContain("TOGGLESLASH_OPERATOR_PASSWORD");
    expect(html).toContain("TOGGLESLASH_SESSION_SECRET");
    expect(html).not.toContain('name="password"');

    const answer = await request.post(`${UNCONFIGURED}/sign-in`, {
      form: { password: E2E_OPERATOR_PASSWORD, returnTo: "/" },
      maxRedirects: 0,
    });
    expect(answer.status()).toBe(303);
    expect(answer.headers()["set-cookie"]).toBeUndefined();

    const url = builtServerFunctions().get("loadEnvironments") ?? "";
    await expectRefused(request, UNCONFIGURED, "loadEnvironments", url);
  });
});

test.describe("over plain http on a LAN address", () => {
  test.use({ storageState: SIGNED_OUT });

  test("the sign-in page says why instead of looping", async ({ request }) => {
    // An IP literal: vite refuses a made-up hostname, and the request URL takes its host from this.
    const headers = { host: `192.0.2.10:${PORT}` };
    const page = await request.get(`${ORIGIN}/sign-in?returnTo=%2F`, { headers });
    const html = await page.text();
    expect(html).toContain("Signing in cannot work over plain http");
    expect(html).not.toContain('name="password"');

    const answer = await request.post(`${ORIGIN}/sign-in`, {
      headers,
      form: { password: E2E_OPERATOR_PASSWORD, returnTo: "/" },
      maxRedirects: 0,
    });
    expect(answer.status()).toBe(303);
    expect(answer.headers()["set-cookie"]).toBeUndefined();
    expect(answer.headers()["location"]).toBe("/sign-in?returnTo=%2F");
  });
});

test("a signed-in request to a server function gets its data", async ({ request }) => {
  const url = builtServerFunctions().get("loadEnvironments") ?? "";
  const response = await request.get(url, { headers: { "x-tsr-serverfn": "true" } });
  expect(response.status()).toBe(200);
  expect(await response.text()).toContain("production");
});

test("sign-out ends access", async ({ page }) => {
  await gotoHydrated(page, "/");
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/sign-in/);
  expect((await page.context().cookies()).filter((c) => c.name === COOKIE)).toEqual([]);
  await gotoHydrated(page, "/keys");
  await expect(page).toHaveURL(/\/sign-in/);
});

test("a session that ends while a page is open sends the next call to sign-in", async ({
  page,
}) => {
  await gotoHydrated(page, "/");
  await expect(section(page, "SDK keys")).toBeVisible();
  await page.context().clearCookies();
  await section(page, "SDK keys").click();
  await expect(page).toHaveURL(/\/sign-in\?returnTo=/);
});

test("no secret is in anything the browser is served", () => {
  const files = filesUnder(CLIENT_BUILD);
  expect(files.length).toBeGreaterThan(2);
  for (const file of files) {
    const text = readFileSync(file, "utf8");
    for (const value of [E2E_ADMIN_TOKEN, E2E_OPERATOR_PASSWORD, E2E_SESSION_SECRET]) {
      expect({ file, leaked: text.includes(value) }).toEqual({ file, leaked: false });
    }
    expect(text).not.toContain("TOGGLESLASH_SESSION_SECRET");
    expect(text).not.toContain("TOGGLESLASH_OPERATOR_PASSWORD");
  }
});
