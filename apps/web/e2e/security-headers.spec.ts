import { expect, test, type Page, type Response } from "@playwright/test";
import { API_PORT, E2E_OPERATOR_PASSWORD } from "./constants.ts";

const API = `http://127.0.0.1:${API_PORT}`;

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

/** What every answer carries, a redirect included. */
function expectHeaders(headers: Record<string, string>) {
  expect(headers["x-content-type-options"]).toBe("nosniff");
  expect(headers["referrer-policy"]).toBe("no-referrer");
  expect(headers["x-frame-options"]).toBe("DENY");
  expect(headers["cache-control"]).toBe("no-store");
  const csp = headers["content-security-policy"] ?? "";
  for (const directive of [
    "default-src 'self'",
    "frame-ancestors 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "object-src 'none'",
  ]) {
    expect(csp.split("; ")).toContain(directive);
  }
  expect(csp).not.toContain("unsafe-inline");
  expect(csp).not.toContain("unsafe-eval");
  expect(csp).toMatch(/script-src 'self' 'nonce-[A-Za-z0-9+/=]{20,}'/);
}

/** Every console error and `securitypolicyviolation` the page raises, collected from before the first load. */
async function watchViolations(page: Page): Promise<string[]> {
  const seen: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error" || /content security policy/i.test(message.text())) {
      seen.push(`${message.type()}: ${message.text()}`);
    }
  });
  page.on("pageerror", (error) => seen.push(`pageerror: ${error.message}`));
  await page.exposeFunction("reportViolation", (what: string) => seen.push(`violation: ${what}`));
  await page.addInitScript(() => {
    document.addEventListener("securitypolicyviolation", (event) => {
      const report: unknown = Reflect.get(window, "reportViolation");
      if (typeof report === "function") report(`${event.violatedDirective} ${event.blockedURI}`);
    });
  });
  return seen;
}

test.describe("signed out", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test("the redirect to sign-in and the sign-in page carry the headers", async ({ request }) => {
    const redirect = await request.get("/keys", { maxRedirects: 0 });
    expect(redirect.status()).toBe(303);
    expectHeaders(redirect.headers());

    const page = await request.get("/sign-in");
    expect(page.status()).toBe(200);
    expectHeaders(page.headers());
  });

  test("each response has its own nonce, and every inline script carries it", async ({ page }) => {
    const responses: Response[] = [];
    page.on("response", (response) => {
      if (response.request().resourceType() === "document") responses.push(response);
    });
    await page.goto("/sign-in");
    await page.reload();
    const nonces = await Promise.all(
      responses.map(
        async (response) =>
          /'nonce-([^']+)'/.exec(
            (await response.allHeaders())["content-security-policy"] ?? "",
          )?.[1],
      ),
    );
    expect(nonces.length).toBeGreaterThanOrEqual(2);
    expect(nonces.every((nonce) => nonce !== undefined)).toBe(true);
    expect(new Set(nonces).size).toBe(nonces.length);

    // The browser hides a nonce attribute once it has parsed it, but the property keeps it.
    const bare = await page.evaluate(
      () =>
        [...document.querySelectorAll<HTMLScriptElement>("script:not([src])")].filter(
          (script) => script.nonce === "",
        ).length,
    );
    expect(bare).toBe(0);
  });

  test("signing in through the form and toggling a flag raise no policy violation", async ({
    page,
  }) => {
    const seen = await watchViolations(page);
    await page.goto("/");
    await expect(page).toHaveURL(/\/sign-in/);
    await page.getByLabel("Password").fill(E2E_OPERATOR_PASSWORD);
    await page.getByTestId("sign-in-submit").click();
    await expect(page.getByTestId("toggle-checkout-v2")).toHaveText("On");
    await page.getByTestId("toggle-checkout-v2").click();
    await expect(page.getByTestId("toggle-checkout-v2")).toHaveText("Off");
    expect(seen).toEqual([]);
  });
});

test("a signed-in page, its client assets and a server function carry the headers", async ({
  page,
}) => {
  const seen = await watchViolations(page);
  const answers: Response[] = [];
  page.on("response", (response) => answers.push(response));
  const document = await page.goto("/");
  await expect(page.getByRole("heading", { name: "Flags" })).toBeVisible();
  expectHeaders((await document?.allHeaders()) ?? {});

  // A client-side navigation calls the server functions; the first load rendered them on the server.
  await page.getByTestId("toggle-checkout-v2").click();
  await expect(page.getByTestId("toggle-checkout-v2")).toHaveText("Off");
  await page.getByTestId("nav-keys").click();
  await expect(page).toHaveURL(/\/keys/);

  const functions = answers.filter((response) => response.url().includes("/_serverFn/"));
  expect(functions.length).toBeGreaterThan(0);
  for (const response of functions) {
    const headers = await response.allHeaders();
    expect(headers["cache-control"]).toBe("no-store");
    expect(headers["x-content-type-options"]).toBe("nosniff");
  }

  const assets = answers.filter((response) =>
    new URL(response.url()).pathname.startsWith("/assets/"),
  );
  expect(assets.length).toBeGreaterThan(0);
  for (const response of assets) {
    const headers = await response.allHeaders();
    expect(headers["x-content-type-options"]).toBe("nosniff");
    expect(headers["x-frame-options"]).toBe("DENY");
    expect(headers["cache-control"]).not.toBe("no-store");
  }
  expect(seen).toEqual([]);
});
