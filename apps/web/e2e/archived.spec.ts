import { expect, test, type Page } from "@playwright/test";
import { API_PORT } from "./constants.ts";
import { gotoHydrated } from "./goto.ts";

const API = `http://127.0.0.1:${API_PORT}`;

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

const flagRow = (page: Page, name: string) => page.getByRole("row", { name: new RegExp(name) });

test("the list and the detail header show each flag's lifecycle stage", async ({ page }) => {
  await gotoHydrated(page, "/");
  await expect(flagRow(page, "Checkout v2").getByText("Active", { exact: true })).toBeVisible();
  await expect(flagRow(page, "Dark mode").getByText("New", { exact: true })).toBeVisible();
  await expect(flagRow(page, "Search boost").getByText("Inactive", { exact: true })).toBeVisible();

  await page.getByRole("link", { name: "Search boost" }).click();
  await expect(page.getByRole("heading", { name: "Search boost", level: 1 })).toBeVisible();
  await expect(page.getByText("Inactive", { exact: true })).toBeVisible();
});

test("choosing Archived shows the archived flags only, and a reload keeps the view", async ({
  page,
}) => {
  await gotoHydrated(page, "/");
  await page.getByRole("radio", { name: "Archived" }).click();
  await expect(page).toHaveURL(/archived=true/);
  await expect(flagRow(page, "Old banner").getByText("Archived", { exact: true })).toBeVisible();
  await expect(flagRow(page, "Legacy cart")).toBeVisible();
  await expect(flagRow(page, "Checkout v2")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Old banner in Production" })).toHaveCount(0);

  await page.reload();
  await expect(page.getByRole("radio", { name: "Archived" })).toBeChecked();
  await expect(flagRow(page, "Old banner")).toBeVisible();
  await expect(flagRow(page, "Checkout v2")).toHaveCount(0);

  await page.getByLabel("Environment").selectOption("staging");
  await expect(page).toHaveURL(/archived=true/);
  await expect(flagRow(page, "Old banner")).toBeVisible();
});

test("Restore moves a flag back to the Active view", async ({ page }) => {
  await gotoHydrated(page, "/?archived=true");
  await page.getByRole("button", { name: "Restore Old banner" }).click();
  await expect(flagRow(page, "Old banner")).toHaveCount(0);

  await page.getByRole("radio", { name: "Active" }).click();
  await expect(flagRow(page, "Old banner")).toBeVisible();
  await expect(page.getByRole("button", { name: "Old banner in Production" })).toHaveText("Off");
});

test("Delete, once confirmed, removes the flag for good", async ({ page }) => {
  await gotoHydrated(page, "/?archived=true");
  await page.getByRole("button", { name: "Delete Old banner" }).click();
  await page
    .getByRole("dialog", { name: "Delete Old banner?" })
    .getByRole("button", { name: "Delete" })
    .click();
  await expect(flagRow(page, "Old banner")).toHaveCount(0);
  await expect(flagRow(page, "Legacy cart")).toBeVisible();

  await page.reload();
  await expect(flagRow(page, "Old banner")).toHaveCount(0);
});
