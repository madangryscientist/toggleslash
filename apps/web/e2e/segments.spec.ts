import { expect, test } from "@playwright/test";
import { API_PORT } from "./constants.ts";

test.beforeEach(async ({ request }) => {
  await request.post(`http://127.0.0.1:${API_PORT}/__reset`);
});

test.describe("creating and deleting segments", () => {
  test("a new segment is created, listed and opened for editing", async ({ page }) => {
    await page.goto("/segments?project=shop&env=production");
    await page.getByTestId("new-segment").click();
    await page.getByLabel("Key").fill("vip");
    await page.getByRole("button", { name: "Create segment" }).click();
    await expect(page.getByTestId("segment-vip")).toContainText("0 contexts");
    await expect(page.getByRole("dialog", { name: "Edit vip" })).toBeVisible();
  });

  test("a bad key is refused before asking, and a taken key shows the server's message", async ({
    page,
  }) => {
    await page.goto("/segments?project=shop&env=production");
    await page.getByTestId("new-segment").click();
    await page.getByRole("button", { name: "Create segment" }).click();
    await expect(page.getByTestId("create-error")).toContainText("Give the segment a key");

    await page.getByLabel("Key").fill("staff");
    await page.getByRole("button", { name: "Create segment" }).click();
    await expect(page.getByTestId("create-error")).toHaveText(
      'Segment "staff" already exists in environment "production".',
    );
    await page.getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByTestId("segments").getByRole("row")).toHaveCount(3);
  });

  test("the empty state offers to create the first segment", async ({ page }) => {
    await page.goto("/segments?project=shop&env=staging");
    await page.getByRole("button", { name: "Create the first segment" }).click();
    await page.getByLabel("Key").fill("first");
    await page.getByRole("button", { name: "Create segment" }).click();
    await expect(page.getByTestId("segment-first")).toBeVisible();
  });

  test("Delete asks first, Cancel keeps the segment, confirming removes it", async ({ page }) => {
    await page.goto("/segments?project=shop&env=production");
    await page.getByTestId("delete-staff").click();
    await expect(page.getByRole("dialog", { name: "Delete staff?" })).toBeVisible();
    await page.getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByTestId("segment-staff")).toBeVisible();

    await page.getByTestId("delete-staff").click();
    await page.getByTestId("confirm-delete-segment").click();
    await expect(page.getByTestId("segment-staff")).toHaveCount(0);
    await expect(page.getByTestId("problem")).toHaveCount(0);
  });

  test("a segment a flag rule names cannot be deleted, and the server says which flag", async ({
    page,
  }) => {
    await page.goto("/segments?project=shop&env=production");
    await page.getByTestId("delete-beta-testers").click();
    await page.getByTestId("confirm-delete-segment").click();
    await expect(page.getByTestId("problem")).toContainText(
      'Segment "beta-testers" is still used by the rules of: search-boost. Remove those rules first.',
    );
    await expect(page.getByTestId("segment-beta-testers")).toBeVisible();
  });
});
