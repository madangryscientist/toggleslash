import { expect, test } from "@playwright/test";
import { API_PORT } from "./constants.ts";

const API = `http://127.0.0.1:${API_PORT}`;

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

test("the list and the detail header show each flag's lifecycle stage", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("flag-checkout-v2").getByTestId("stage")).toHaveText("Active");
  await expect(page.getByTestId("flag-dark-mode").getByTestId("stage")).toHaveText("New");
  await expect(page.getByTestId("flag-search-boost").getByTestId("stage")).toHaveText("Inactive");

  await page.getByTestId("open-search-boost").click();
  await expect(page.getByRole("heading", { name: "Search boost" })).toBeVisible();
  await expect(page.getByTestId("stage")).toHaveText("Inactive");
});

test("choosing Archived shows the archived flags only, and a reload keeps the view", async ({
  page,
}) => {
  await page.goto("/");
  await page.getByRole("radio", { name: "Archived" }).click();
  await expect(page).toHaveURL(/archived=true/);
  await expect(page.getByTestId("flag-old-banner").getByTestId("stage")).toHaveText("Archived");
  await expect(page.getByTestId("flag-legacy-cart")).toBeVisible();
  await expect(page.getByTestId("flag-checkout-v2")).toHaveCount(0);
  await expect(page.getByTestId("toggle-old-banner")).toHaveCount(0);

  await page.reload();
  await expect(page.getByRole("radio", { name: "Archived" })).toBeChecked();
  await expect(page.getByTestId("flag-old-banner")).toBeVisible();
  await expect(page.getByTestId("flag-checkout-v2")).toHaveCount(0);

  await page.getByLabel("Environment").selectOption("staging");
  await expect(page).toHaveURL(/archived=true/);
  await expect(page.getByTestId("flag-old-banner")).toBeVisible();
});

test("the archived view says so when the project has none", async ({ page }) => {
  await page.goto("/?project=docs&archived=true");
  await expect(page.getByText("No archived flags in Docs.")).toBeVisible();
});

test("Restore moves a flag back to the Active view", async ({ page }) => {
  await page.goto("/?archived=true");
  await page.getByTestId("flag-old-banner").getByTestId("restore-flag").click();
  await expect(page.getByTestId("flag-old-banner")).toHaveCount(0);

  await page.getByRole("radio", { name: "Active" }).click();
  await expect(page.getByTestId("flag-old-banner")).toBeVisible();
  await expect(page.getByTestId("toggle-old-banner")).toHaveText("Off");
});

test("Delete asks first: Cancel keeps the flag, confirming removes it", async ({ page }) => {
  await page.goto("/?archived=true");
  await page.getByTestId("flag-old-banner").getByTestId("delete-flag").click();
  await expect(page.getByText("Delete Old banner?")).toBeVisible();
  await page.getByRole("button", { name: "Cancel" }).click();
  await expect(page.getByTestId("flag-old-banner")).toBeVisible();

  await page.getByTestId("flag-old-banner").getByTestId("delete-flag").click();
  await page.getByTestId("confirm-delete").click();
  await expect(page.getByTestId("flag-old-banner")).toHaveCount(0);
  await expect(page.getByTestId("flag-legacy-cart")).toBeVisible();

  await page.reload();
  await expect(page.getByTestId("flag-old-banner")).toHaveCount(0);
});

test("Delete on a flag another flag needs shows the server's message and keeps it", async ({
  page,
}) => {
  await page.goto("/?archived=true");
  await page.getByTestId("flag-legacy-cart").getByTestId("delete-flag").click();
  await page.getByTestId("confirm-delete").click();
  await expect(page.getByTestId("problem")).toContainText('"search-boost"');
  await expect(page.getByTestId("problem")).toContainText("prerequisite");
  await expect(page.getByTestId("flag-legacy-cart")).toBeVisible();
});
