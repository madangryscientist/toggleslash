import { expect, test } from "@playwright/test";
import { API_PORT, E2E_ADMIN_TOKEN } from "./constants.ts";
import { waitForHydrated } from "./hydrated.ts";

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

test.describe("members of other context kinds", () => {
  test("an org member is saved and shown after a reload, and counted", async ({
    page,
    request,
  }) => {
    await page.goto("/segments?project=shop&env=production");
    await page.getByTestId("edit-beta-testers").click();
    await page.locator("#segment-included-kinds").fill("org:acme\ndevice:d1\norg:globex");
    await page.locator("#segment-excluded-kinds").fill("org:evil");
    await page.getByTestId("save-segment").click();
    await expect(page.getByTestId("segment-beta-testers")).toContainText("5 contexts");

    await page.reload();
    await expect(page.getByTestId("segment-beta-testers")).toContainText("5 contexts");
    await page.getByTestId("edit-beta-testers").click();
    await expect(page.locator("#segment-included-kinds")).toHaveValue(
      "org:acme\norg:globex\ndevice:d1",
    );
    await expect(page.locator("#segment-excluded-kinds")).toHaveValue("org:evil");
    // The user boxes were not touched.
    await expect(page.locator("#segment-included")).toHaveValue("alice\nbob");

    const stored = await request.get(
      `http://127.0.0.1:${API_PORT}/api/v1/projects/shop/environments/production/segments`,
      {
        headers: { authorization: `Bearer ${E2E_ADMIN_TOKEN}` },
      },
    );
    const beta = (await stored.json()).find((s: { key: string }) => s.key === "beta-testers");
    expect(beta.includedContexts).toEqual([
      { contextKind: "org", values: ["acme", "globex"] },
      { contextKind: "device", values: ["d1"] },
    ]);
  });

  test("an invalid line names itself and nothing is sent", async ({ page }) => {
    await page.goto("/segments?project=shop&env=production");
    await page.getByTestId("edit-beta-testers").click();
    await waitForHydrated(page, "#segment-included");
    await expect(page.locator("#segment-included")).toHaveValue("alice\nbob");
    await page.locator("#segment-included-kinds").fill("org:acme\nacme");
    await page.getByTestId("save-segment").click();
    await expect(page.getByTestId("segment-error")).toContainText("Line 2");
    await expect(page.getByTestId("segment-error")).toContainText("Included, other kinds");

    await page.locator("#segment-included-kinds").fill("user:alice");
    await page.getByTestId("save-segment").click();
    await expect(page.getByTestId("segment-error")).toContainText("Line 1");

    // Nothing was saved: the dialog is still open and the count is unchanged behind it.
    await expect(page.getByTestId("save-segment")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByTestId("save-segment")).toHaveCount(0);
    await expect(page.getByTestId("segment-beta-testers")).toContainText("2 contexts");
    await expect(page.getByTestId("segment-beta-testers")).toContainText("1 context");
  });
});
