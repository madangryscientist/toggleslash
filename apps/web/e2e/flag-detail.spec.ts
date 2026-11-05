import { expect, test, type Page } from "@playwright/test";
import { API_PORT, E2E_ADMIN_TOKEN } from "./constants.ts";

const API = `http://127.0.0.1:${API_PORT}`;
const CONFIG = `${API}/api/v1/projects/shop/flags/checkout-v2/environments/production`;
const AUTH = { authorization: `Bearer ${E2E_ADMIN_TOKEN}` };

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

async function openCheckout(page: Page) {
  await page.goto("/flags/checkout-v2?project=shop&env=production");
  await expect(page.getByRole("heading", { name: "Checkout v2" })).toBeVisible();
}

test("opens from the flag list and shows the flag's targeting", async ({ page }) => {
  await page.goto("/");
  await page.getByTestId("open-checkout-v2").click();
  await expect(page).toHaveURL(/\/flags\/checkout-v2/);
  await expect(page.getByTestId("meta-name")).toHaveValue("Checkout v2");
  await expect(page.getByTestId("targeting-on")).toBeChecked();
  await expect(page.getByTestId("targets-0")).toHaveValue(/alice/);
  await expect(page.getByTestId("rule-0-clause-0-attribute")).toHaveValue("country");
  await expect(page.getByTestId("rule-0-percent-0")).toHaveValue("25");
  await expect(page.getByTestId("rule-0-percent-1")).toHaveValue("75");
  await expect(page.getByText("Version 3", { exact: true })).toBeVisible();
});

test("edits a rule and a rollout, saves, and sees them after a reload", async ({
  page,
  request,
}) => {
  await openCheckout(page);
  await expect(page.getByTestId("save-targeting")).toBeDisabled();

  await page.getByTestId("rule-0-clause-0-values").fill("NL, DE, BE");
  await page.getByTestId("rule-0-percent-0").fill("40");
  await page.getByTestId("rule-0-percent-1").fill("60");
  await expect(page.getByTestId("rule-0-total")).toContainText("100%");
  await page.getByTestId("save-targeting").click();

  await expect(page.getByTestId("notice")).toContainText("Targeting saved");
  await expect(page.getByText("Version 4", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByTestId("rule-0-clause-0-values")).toHaveValue(/BE/);
  await expect(page.getByTestId("rule-0-percent-0")).toHaveValue("40");
  await expect(page.getByTestId("rule-0-percent-1")).toHaveValue("60");

  // The server has it, not just the page.
  const stored = await request.get(CONFIG, { headers: AUTH });
  expect(await stored.json()).toMatchObject({
    version: 4,
    rules: [
      {
        clauses: [{ op: "in", attribute: "country", values: ["NL", "DE", "BE"] }],
        rollout: {
          variations: [
            { variation: 0, weight: 40000 },
            { variation: 1, weight: 60000 },
          ],
        },
      },
    ],
  });
});

test("adds a rule and a prerequisite and turns targeting off", async ({ page, request }) => {
  await openCheckout(page);
  await page.getByTestId("add-rule").click();
  await page.getByTestId("rule-1-clause-0-attribute").fill("plan");
  await page.getByTestId("rule-1-clause-0-values").fill("pro");
  await page.getByTestId("rule-1-variation").selectOption("0");
  await page.getByTestId("add-prerequisite").click();
  await page.getByTestId("prerequisite-0-flag").selectOption("dark-mode");
  await page.getByTestId("targeting-on").uncheck();
  await page.getByTestId("save-targeting").click();
  await expect(page.getByTestId("notice")).toContainText("Targeting saved");

  const stored = await (await request.get(CONFIG, { headers: AUTH })).json();
  expect(stored).toMatchObject({
    on: false,
    prerequisites: [{ flagKey: "dark-mode", variation: 0 }],
  });
  expect(stored.rules).toHaveLength(2);
});

test("a rollout that does not total 100% is shown at the rule, and the draft is kept", async ({
  page,
}) => {
  await openCheckout(page);
  await page.getByTestId("rule-0-percent-0").fill("30");
  await expect(page.getByTestId("rule-0-total")).toContainText("105%");
  await page.getByTestId("save-targeting").click();

  await expect(page.getByTestId("rule-0")).toContainText("weights must sum to 100000");
  await expect(page.getByTestId("config-error")).toBeVisible();
  await expect(page.getByTestId("rule-0-percent-0")).toHaveValue("30");

  // Fixing it clears the message.
  await page.getByTestId("rule-0-percent-1").fill("70");
  await expect(page.getByTestId("config-error")).toHaveCount(0);
  await page.getByTestId("save-targeting").click();
  await expect(page.getByTestId("notice")).toContainText("Targeting saved");
});

test("a clause with no attribute is refused by the server at that clause", async ({ page }) => {
  await openCheckout(page);
  await page.getByTestId("rule-0-clause-0-attribute").fill("");
  await page.getByTestId("save-targeting").click();
  await expect(page.getByTestId("rule-0")).toContainText("attribute must be a non-empty string");
});

test("a percentage that is not a number is caught before it is sent", async ({ page }) => {
  await openCheckout(page);
  await page.getByTestId("rule-0-percent-0").fill("lots");
  await page.getByTestId("save-targeting").click();
  await expect(page.getByTestId("rule-0")).toContainText("A percentage from 0 to 100");
  await expect(page.getByTestId("config-error")).toBeVisible();
});

test("a version conflict is shown and the latest config is loaded", async ({ page, request }) => {
  await openCheckout(page);
  await page.getByTestId("rule-0-clause-0-values").fill("NL, DE, BE");
  // Someone else saves after this page loaded.
  await request.post(`${API}/__bump/shop/production/checkout-v2`);
  await page.getByTestId("save-targeting").click();

  await expect(page.getByTestId("notice")).toContainText("Someone else changed this flag");
  await expect(page.getByTestId("notice")).toContainText("not saved");
  await expect(page.getByText("Version 4", { exact: true })).toBeVisible();
  // The draft was discarded in favour of what the server has.
  await expect(page.getByTestId("rule-0-clause-0-values")).not.toHaveValue(/BE/);
  await expect(page.getByTestId("save-targeting")).toBeDisabled();
});

test("edits the flag's details and sees a field error from the server", async ({ page }) => {
  await openCheckout(page);
  await page.getByTestId("meta-name").fill("");
  await page.getByTestId("save-meta").click();
  await expect(page.getByTestId("meta-form")).toContainText("name must not be empty");

  await page.getByTestId("meta-name").fill("Checkout v2 (new)");
  await page.getByTestId("meta-owner").fill("payments");
  await page.getByTestId("meta-tags").fill("checkout, money");
  await page.getByTestId("save-meta").click();
  await expect(page.getByTestId("notice")).toContainText("Details saved");

  await page.reload();
  await expect(page.getByTestId("meta-name")).toHaveValue("Checkout v2 (new)");
  await expect(page.getByTestId("meta-owner")).toHaveValue("payments");
  await expect(page.getByTestId("meta-tags")).toHaveValue("checkout, money");
});

test("shows the flag's history and usage", async ({ page }) => {
  await openCheckout(page);
  await expect(page.getByTestId("history-event")).toHaveCount(3);
  await expect(page.getByTestId("history")).toContainText("2026-11-04 10:00:00 UTC");
  await expect(page.getByTestId("usage-total-production")).toContainText("42");
  await expect(page.getByTestId("usage-production")).toContainText("2026-11-04");
  await expect(page.getByTestId("usage-production")).toContainText("true: 10, false: 20");
});

test("a save shows up in the history", async ({ page }) => {
  await openCheckout(page);
  await page.getByTestId("targeting-on").uncheck();
  await page.getByTestId("save-targeting").click();
  await expect(page.getByTestId("history-event")).toHaveCount(4);
});

test("an unknown flag says so instead of crashing", async ({ page }) => {
  const response = await page.goto("/flags/nope?project=shop&env=production");
  expect(response?.status()).toBeLessThan(500);
  await expect(page.getByTestId("server-down")).toBeVisible();
});

test("the admin token is not in the detail page", async ({ page }) => {
  await openCheckout(page);
  await page.waitForLoadState("networkidle");
  expect(await page.content()).not.toContain(E2E_ADMIN_TOKEN);
});
