import { expect, test, type Page } from "@playwright/test";
import { API_PORT } from "./constants.ts";
import { gotoHydrated } from "./goto.ts";

test.beforeEach(async ({ request }) => {
  await request.post(`http://127.0.0.1:${API_PORT}/__reset`);
});

const segmentRow = (page: Page, key: string) =>
  page.getByRole("row", { name: new RegExp(`^${key} `) });

test("a new segment is created, listed and opened for editing", async ({ page }) => {
  await gotoHydrated(page, "/segments?project=shop&env=production");
  await page.getByRole("button", { name: "New segment" }).click();
  const dialog = page.getByRole("dialog", { name: "New segment" });
  await dialog.getByLabel("Key").fill("vip");
  await dialog.getByRole("button", { name: "Create segment" }).click();
  await expect(segmentRow(page, "vip")).toContainText("0 contexts");
  await expect(page.getByRole("dialog", { name: "Edit vip" })).toBeVisible();
});

test("Delete, once confirmed, removes the segment", async ({ page }) => {
  await gotoHydrated(page, "/segments?project=shop&env=production");
  await page.getByRole("button", { name: "Delete staff" }).click();
  await page
    .getByRole("dialog", { name: "Delete staff?" })
    .getByRole("button", { name: "Delete" })
    .click();
  await expect(segmentRow(page, "staff")).toHaveCount(0);
  await page.reload();
  await expect(segmentRow(page, "staff")).toHaveCount(0);
  await expect(segmentRow(page, "beta-testers")).toBeVisible();
});

test("members of other kinds are saved, counted and shown after a reload", async ({ page }) => {
  await gotoHydrated(page, "/segments?project=shop&env=production");
  await page.getByRole("button", { name: "Edit beta-testers" }).click();
  const dialog = page.getByRole("dialog", { name: "Edit beta-testers" });
  await dialog.getByLabel("Included, other kinds").fill("org:acme\ndevice:d1\norg:globex");
  await dialog.getByLabel("Excluded, other kinds").fill("org:evil");
  await dialog.getByRole("button", { name: "Save segment" }).click();
  await expect(segmentRow(page, "beta-testers")).toContainText("5 contexts");

  await page.reload();
  await expect(segmentRow(page, "beta-testers")).toContainText("5 contexts");
  await page.getByRole("button", { name: "Edit beta-testers" }).click();
  await expect(dialog.getByLabel("Included, other kinds")).toHaveValue(
    "org:acme\norg:globex\ndevice:d1",
  );
  await expect(dialog.getByLabel("Excluded, other kinds")).toHaveValue("org:evil");
  // The user boxes were not touched.
  await expect(dialog.getByLabel("Always included")).toHaveValue("alice\nbob");
});
