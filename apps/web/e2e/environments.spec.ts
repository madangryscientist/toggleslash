import { expect, test, type APIRequestContext } from "@playwright/test";
import { API_PORT, E2E_ADMIN_TOKEN } from "./constants.ts";

const api = `http://127.0.0.1:${API_PORT}`;

test.beforeEach(async ({ request }) => {
  await request.post(`${api}/__reset`);
});

/** The fake API's environment list, as `key:name` pairs, the way the dashboard would read it. */
async function listed(request: APIRequestContext): Promise<string[]> {
  const answer = await request.get(`${api}/api/v1/projects/shop/environments`, {
    headers: { authorization: `Bearer ${E2E_ADMIN_TOKEN}` },
  });
  const body: unknown = await answer.json();
  if (!Array.isArray(body)) return [];
  return body.map((e: { key: string; name: string }) => `${e.key}:${e.name}`);
}

test.describe("renaming and deleting environments", () => {
  test("renaming keeps the key, shows the new name, and survives a reload", async ({
    page,
    request,
  }) => {
    await page.goto("/environments?project=shop");
    await page.getByTestId("rename-staging").click();
    await expect(page.getByRole("dialog", { name: "Rename Staging" })).toBeVisible();
    await page.locator("#env-rename").fill("Pre-production");
    await page.getByRole("button", { name: "Rename", exact: true }).click();

    const row = page.getByTestId("environment-staging");
    await expect(row).toContainText("Pre-production");
    await expect(row).toContainText("staging");
    expect(await listed(request)).toContain("staging:Pre-production");
    await page.reload();
    await expect(page.getByTestId("environment-staging")).toContainText("Pre-production");
  });

  test("a blank name is refused before any request", async ({ page }) => {
    await page.goto("/environments?project=shop");
    await page.getByTestId("rename-staging").click();
    await page.locator("#env-rename").fill("   ");
    await page.getByRole("button", { name: "Rename", exact: true }).click();
    await expect(page.getByTestId("rename-error")).toHaveText("Give the environment a name.");
    await page.getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByTestId("environment-staging")).toContainText("Staging");
  });

  test("Delete asks first, Cancel keeps the environment, confirming removes it", async ({
    page,
    request,
  }) => {
    await page.goto("/environments?project=shop");
    await page.getByTestId("delete-staging").click();
    await expect(page.getByRole("dialog", { name: "Delete staging?" })).toBeVisible();
    await page.getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByTestId("environment-staging")).toBeVisible();

    await page.getByTestId("delete-staging").click();
    await page.getByTestId("confirm-delete-environment").click();
    await expect(page.getByTestId("environment-staging")).toHaveCount(0);
    await expect(page.getByTestId("problem")).toHaveCount(0);
    expect(await listed(request)).toEqual(["production:Production"]);
  });

  test("an environment with a live key cannot be deleted, and the server says so", async ({
    page,
  }) => {
    await page.goto("/environments?project=shop");
    await page.getByTestId("delete-production").click();
    await page.getByTestId("confirm-delete-environment").click();
    await expect(page.getByTestId("problem")).toContainText('Environment "production" still has');
    await expect(page.getByTestId("problem")).toContainText("Revoke them first.");
    await expect(page.getByTestId("environment-production")).toBeVisible();
  });
});
