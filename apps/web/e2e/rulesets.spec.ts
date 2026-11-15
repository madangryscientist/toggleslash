import { expect, test } from "@playwright/test";
import { API_PORT, PAST_RULESET_HASH, PLANTED_SALT } from "./constants.ts";
import { gotoHydrated } from "./goto.ts";

const API = `http://127.0.0.1:${API_PORT}`;
const PAGE = `/rulesets/${PAST_RULESET_HASH}?project=shop&env=production`;

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

test("shows the flags as they were under the hash, not as they are now", async ({ page }) => {
  await gotoHydrated(page, PAGE);
  await expect(page.getByRole("heading", { name: "Flags under a past ruleset" })).toBeVisible();
  await expect(page.getByText(PAST_RULESET_HASH)).toBeVisible();
  // checkout-v2 was off with a country rule, dark-mode was on: the fake's current configs say otherwise.
  const checkout = page.getByRole("region", { name: "checkout-v2" });
  await expect(checkout.getByText("Off", { exact: true })).toBeVisible();
  await expect(checkout).toContainText('country in "NL" → serve 0 (true)');
  const darkMode = page.getByRole("region", { name: "dark-mode" });
  await expect(darkMode.getByText("On", { exact: true })).toBeVisible();
  await expect(page.getByRole("region", { name: "Segment beta-testers" })).toContainText(
    "alice, org:acme",
  );
});

test("the page the server renders has no salt and no token", async ({ request }) => {
  const answer = await request.get(PAGE);
  expect(answer.status()).toBe(200);
  // The server render is the document the browser is sent, and where a leaked loader value would show up.
  const html = await answer.text();
  expect(html).toContain("checkout-v2");
  expect(html).not.toContain(PLANTED_SALT);
  expect(html.toLowerCase()).not.toContain("salt");
  // A header has "bearer " with the space; a generated name or nonce cannot.
  expect(html.toLowerCase()).not.toContain("bearer ");
});

test.describe("signed out", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test("is not reachable, leaks no flag data, and sends sign-in back to the same URL", async ({
    request,
  }) => {
    const url = `${PAGE}&flag=checkout-v2`;
    const refused = await request.get(url, { maxRedirects: 0 });
    expect(refused.status()).toBe(303);
    const body = await refused.text();
    for (const leak of ["checkout-v2", "dark-mode", "beta-testers", PAST_RULESET_HASH, "NL"]) {
      expect(body).not.toContain(leak);
    }
    const location = new URL(refused.headers()["location"] ?? "", "http://dashboard");
    expect(location.pathname).toBe("/sign-in");
    expect(location.searchParams.get("returnTo")).toBe(url);
  });
});
