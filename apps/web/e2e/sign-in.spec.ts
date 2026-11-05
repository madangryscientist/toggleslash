import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { expect, test, type APIRequestContext, type Page } from "@playwright/test";
import {
  API_PORT,
  E2E_ADMIN_TOKEN,
  E2E_OPERATOR_PASSWORD,
  E2E_SESSION_SECRET,
  PORT,
  UNCONFIGURED_PORT,
} from "./constants.ts";

const API = `http://127.0.0.1:${API_PORT}`;
const UNCONFIGURED = `http://localhost:${UNCONFIGURED_PORT}`;
const ORIGIN = `http://localhost:${PORT}`;
const CLIENT_BUILD = join(import.meta.dirname, "../dist/client");
const COOKIE = "featureslash_session";

test.use({ storageState: { cookies: [], origins: [] } });

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

async function signIn(page: Page, password = E2E_OPERATOR_PASSWORD) {
  await page.getByLabel("Password").fill(password);
  await page.getByTestId("sign-in-submit").click();
}

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

test("a signed-out page redirects to sign-in, and returns there afterwards", async ({ page }) => {
  await page.goto("/keys?project=shop&env=staging");
  await expect(page).toHaveURL(/\/sign-in\?returnTo=%2Fkeys%3Fproject%3Dshop%26env%3Dstaging/);
  await signIn(page);
  await expect(page).toHaveURL(/\/keys\?project=shop&env=staging$/);
  await expect(page.getByLabel("Environment")).toHaveValue("staging");
});

test("a wrong password stays signed out, and says only that it failed", async ({ page }) => {
  await page.goto("/");
  await signIn(page, "not the password at all");
  await expect(page).toHaveURL(/\/sign-in\?failed=true/);
  await expect(page.getByTestId("sign-in-failed")).toHaveText(
    /That did not work\s*Check the password and try again\./,
  );
  expect(page.url()).not.toContain("password");
  expect((await page.context().cookies()).filter((c) => c.name === COOKIE)).toEqual([]);
  await page.goto("/");
  await expect(page).toHaveURL(/\/sign-in/);
});

test("sign-out ends access", async ({ page }) => {
  await page.goto("/");
  await signIn(page);
  await expect(page.getByRole("heading", { name: "Flags" })).toBeVisible();
  await page.getByTestId("sign-out").click();
  await expect(page).toHaveURL(/\/sign-in/);
  expect((await page.context().cookies()).filter((c) => c.name === COOKIE)).toEqual([]);
  await page.goto("/keys");
  await expect(page).toHaveURL(/\/sign-in/);
});

test("a session that ends while a page is open sends the next call to sign-in", async ({
  page,
}) => {
  await page.goto("/");
  await signIn(page);
  await expect(page.getByTestId("nav-keys")).toBeVisible();
  await page.context().clearCookies();
  await page.getByTestId("nav-keys").click();
  await expect(page).toHaveURL(/\/sign-in\?returnTo=/);
});

test("signed-out direct requests to every server function return no data", async ({ request }) => {
  const functions = builtServerFunctions();
  expect([...functions.keys()].toSorted()).toEqual(declaredServerFunctions().toSorted());
  for (const [name, url] of functions) {
    await expectRefused(request, ORIGIN, name, url);
  }
});

test("the same request with a real session gets its data", async ({ page }) => {
  await page.goto("/");
  await signIn(page);
  await expect(page.getByRole("heading", { name: "Flags" })).toBeVisible();
  const url = builtServerFunctions().get("loadEnvironments") ?? "";
  const response = await page.context().request.get(url, {
    headers: { "x-tsr-serverfn": "true" },
  });
  expect(response.status()).toBe(200);
  expect(await response.text()).toContain("production");
});

test("a tampered cookie is refused by pages and by server functions", async ({ page }) => {
  await page.goto("/");
  await signIn(page);
  await expect(page.getByRole("heading", { name: "Flags" })).toBeVisible();
  const context = page.context();
  const real = (await context.cookies()).find((c) => c.name === COOKIE);
  if (real === undefined) throw new Error("no session cookie");

  const [expiry, signature = ""] = real.value.split(".");
  const flipped = `${signature.startsWith("0") ? "1" : "0"}${signature.slice(1)}`;
  await context.clearCookies();
  await context.addCookies([{ ...real, value: `${expiry ?? ""}.${flipped}` }]);
  await page.goto("/");
  await expect(page).toHaveURL(/\/sign-in/);

  const url = builtServerFunctions().get("loadEnvironments") ?? "";
  const response = await context.request.get(url);
  expect(response.status()).toBe(401);
});

test.describe("with neither secret set", () => {
  test("the sign-in page names the variables and nobody gets in", async ({ page, request }) => {
    await page.goto(`${UNCONFIGURED}/`);
    await expect(page).toHaveURL(/\/sign-in/);
    const message = page.getByTestId("sign-in-unconfigured");
    await expect(message).toContainText("FEATURESLASH_OPERATOR_PASSWORD");
    await expect(message).toContainText("FEATURESLASH_SESSION_SECRET");
    await expect(page.getByLabel("Password")).toHaveCount(0);

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

test("no secret is in anything the browser is served", () => {
  const files = filesUnder(CLIENT_BUILD);
  expect(files.length).toBeGreaterThan(2);
  for (const file of files) {
    const text = readFileSync(file, "utf8");
    for (const value of [E2E_ADMIN_TOKEN, E2E_OPERATOR_PASSWORD, E2E_SESSION_SECRET]) {
      expect({ file, leaked: text.includes(value) }).toEqual({ file, leaked: false });
    }
    expect(text).not.toContain("FEATURESLASH_SESSION_SECRET");
    expect(text).not.toContain("FEATURESLASH_OPERATOR_PASSWORD");
  }
});
