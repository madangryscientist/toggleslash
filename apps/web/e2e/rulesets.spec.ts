import { expect, test } from "@playwright/test";
import { API_PORT, E2E_OPERATOR_PASSWORD, PAST_RULESET_HASH, PLANTED_SALT } from "./constants.ts";

const API = `http://127.0.0.1:${API_PORT}`;
const PAGE = `/rulesets/${PAST_RULESET_HASH}?project=shop&env=production`;

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

test("shows the flags as they were under the hash, not as they are now", async ({ page }) => {
  await page.goto(PAGE);
  await expect(page.getByRole("heading", { name: "Flags under a past ruleset" })).toBeVisible();
  await expect(page.getByTestId("ruleset-hash")).toContainText(PAST_RULESET_HASH);
  // checkout-v2 was off with a country rule, dark-mode was on: the fake's current configs say otherwise.
  await expect(page.getByTestId("past-on-checkout-v2")).toHaveText("Off");
  await expect(page.getByTestId("past-on-dark-mode")).toHaveText("On");
  await expect(page.getByTestId("past-rule-checkout-v2-0")).toContainText(
    'country in "NL" → serve 0 (true)',
  );
  await expect(page.getByTestId("past-segment-beta-testers")).toBeVisible();
  await expect(page.getByTestId("past-segment-beta-testers")).toContainText("alice, org:acme");
});

test("shows no salt, and no token is in the page or its address", async ({ page }) => {
  const response = await page.goto(PAGE);
  await expect(page.getByTestId("past-flag-checkout-v2")).toBeVisible();
  // The server render is the document the browser was sent, and it is where a leaked loader value would show up.
  // The DOM after hydration is checked too; the loader data is not in it, so it is the weaker of the two.
  const served = (await response?.text()) ?? "";
  expect(served).toContain("checkout-v2");
  const rendered = await page.content();
  for (const html of [served, rendered]) {
    expect(html).not.toContain(PLANTED_SALT);
    expect(html).not.toContain('"salt"');
    // A header has "bearer " with the space; a generated name or nonce cannot.
    expect(html.toLowerCase()).not.toContain("bearer ");
  }
  // A generated name or nonce can hold the letters, but not the bare word as visible text.
  const visible = (await page.locator("body").innerText()).toLowerCase();
  expect(visible).not.toContain("salt");
  expect(page.url()).not.toMatch(/token|authorization/i);
});

test("?flag= narrows it to that flag", async ({ page }) => {
  await page.goto(`${PAGE}&flag=dark-mode`);
  await expect(page.getByTestId("past-flag-dark-mode")).toBeVisible();
  await expect(page.getByTestId("past-flag-checkout-v2")).toHaveCount(0);
});

test("a hash the environment never served is a clear not-found, not an error page", async ({
  page,
}) => {
  await page.goto(`/rulesets/${"0".repeat(64)}?project=shop&env=production`);
  await expect(page.getByTestId("ruleset-not-found")).toContainText(
    "No ruleset found for that hash",
  );
  await expect(page.getByTestId("past-flag-checkout-v2")).toHaveCount(0);
  await expect(page.getByTestId("server-down")).toHaveCount(0);
});

test("a flag the hash's ruleset lacks says so, not that the hash was never served", async ({
  page,
}) => {
  await page.goto(`${PAGE}&flag=nope`);
  const notFound = page.getByTestId("ruleset-not-found");
  await expect(notFound).toContainText("That ruleset has no such flag.");
  await expect(notFound).not.toContainText("never served");
});

test("without a project and an environment it says what is missing", async ({ page }) => {
  await page.goto(`/rulesets/${PAST_RULESET_HASH}`);
  await expect(page.getByTestId("ruleset-needs-scope")).toBeVisible();
  await expect(page.getByTestId("past-flag-checkout-v2")).toHaveCount(0);
});

test.describe("signed out", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test("is not reachable, leaks no flag data, and returns to the same URL after sign-in", async ({
    page,
    request,
  }) => {
    const refused = await request.get(`${PAGE}&flag=checkout-v2`, { maxRedirects: 0 });
    expect(refused.status()).toBe(303);
    const body = await refused.text();
    for (const leak of ["checkout-v2", "dark-mode", "beta-testers", PAST_RULESET_HASH, "NL"]) {
      expect(body).not.toContain(leak);
    }

    await page.goto(PAGE);
    await expect(page).toHaveURL(/\/sign-in\?returnTo=/);
    await expect(page.getByTestId("past-flag-checkout-v2")).toHaveCount(0);
    await page.getByLabel("Password").fill(E2E_OPERATOR_PASSWORD);
    await page.getByTestId("sign-in-submit").click();
    await expect(page).toHaveURL(new RegExp(`${PAGE.replace("?", "\\?")}$`));
    await expect(page.getByTestId("past-flag-checkout-v2")).toBeVisible();
  });
});
