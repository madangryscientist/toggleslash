import { expect, test, type Page } from "@playwright/test";
import { API_PORT } from "./constants.ts";
import { gotoHydrated } from "./goto.ts";

const API = `http://127.0.0.1:${API_PORT}`;

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

/** A flag's row in the list, found by its name. */
const flagRow = (page: Page, name: string) => page.getByRole("row", { name: new RegExp(name) });
/** A flag's on/off button, named for the flag and the environment it switches. */
const toggle = (page: Page, name: string, env = "Production") =>
  page.getByRole("button", { name: `${name} in ${env}` });

test("lists the first project's flags in its first environment", async ({ page }) => {
  await gotoHydrated(page, "/");
  await expect(page.getByRole("heading", { name: "Flags" })).toBeVisible();
  await expect(page.getByLabel("Project")).toHaveValue("shop");
  await expect(page.getByLabel("Environment")).toHaveValue("production");
  const checkout = flagRow(page, "Checkout v2");
  await expect(checkout).toContainText("temporary");
  await expect(checkout).toContainText("2026-11-04");
  await expect(flagRow(page, "Dark mode")).toContainText("never");
  await expect(toggle(page, "Checkout v2")).toHaveText("On");
  await expect(toggle(page, "Dark mode")).toHaveText("Off");
});

test("switches environment and project", async ({ page }) => {
  await gotoHydrated(page, "/");
  await page.getByLabel("Environment").selectOption("staging");
  await expect(page).toHaveURL(/env=staging/);
  await expect(toggle(page, "Dark mode", "Staging")).toHaveText("On");

  await page.getByLabel("Project").selectOption("docs");
  await expect(page.getByLabel("Environment")).toHaveValue("live");
  await expect(flagRow(page, "Docs search")).toBeVisible();
  await expect(flagRow(page, "Checkout v2")).toHaveCount(0);
});

test("toggling a flag changes it on the server and keeps it after a reload", async ({ page }) => {
  await gotoHydrated(page, "/");
  await toggle(page, "Dark mode").click();
  await expect(toggle(page, "Dark mode")).toHaveText("On");

  await page.reload();
  await expect(toggle(page, "Dark mode")).toHaveText("On");
});

test("creates a flag", async ({ page }) => {
  await gotoHydrated(page, "/");
  await page.getByRole("button", { name: "New flag" }).click();
  const dialog = page.getByRole("dialog", { name: "New flag" });
  await dialog.getByLabel("Name").fill("Fast lane");
  await dialog.getByLabel("Key", { exact: true }).fill("fast-lane");
  await dialog.getByLabel("Tags").fill("perf, checkout");
  await dialog.getByLabel("Temporary").check();
  await dialog.getByLabel("Visible to client keys").check();
  await dialog.getByRole("button", { name: "Create flag" }).click();

  const row = flagRow(page, "Fast lane");
  await expect(row).toContainText("temporary");
  await expect(row).toContainText("perf");
  await expect(toggle(page, "Fast lane")).toHaveText("Off");

  await row.getByRole("link", { name: "Fast lane" }).click();
  await expect(page.getByLabel("Name")).toHaveValue("Fast lane");
  await expect(page.getByLabel("Visible to client keys")).toBeChecked();
  await expect(page.getByLabel("Temporary")).toBeChecked();
});

test("archives a flag after confirming, and it stays archived after a reload", async ({ page }) => {
  await gotoHydrated(page, "/");
  await page.getByRole("button", { name: "Archive Dark mode" }).click();
  await page
    .getByRole("dialog", { name: "Archive Dark mode?" })
    .getByRole("button", { name: "Archive" })
    .click();
  await expect(flagRow(page, "Dark mode")).toHaveCount(0);
  await expect(flagRow(page, "Checkout v2")).toBeVisible();

  await page.reload();
  await expect(flagRow(page, "Dark mode")).toHaveCount(0);
});
