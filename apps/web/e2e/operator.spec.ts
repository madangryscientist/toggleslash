import { expect, test } from "@playwright/test";
import { API_PORT, E2E_ADMIN_TOKEN } from "./constants.ts";

const API = `http://127.0.0.1:${API_PORT}`;
const AUTH = { authorization: `Bearer ${E2E_ADMIN_TOKEN}` };

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

test("the nav moves between the operator pages and keeps the project and environment", async ({
  page,
}) => {
  await page.goto("/?project=shop&env=staging");
  await page.getByTestId("nav-environments").click();
  await expect(page).toHaveURL(/\/environments\?project=shop&env=staging/);
  await expect(page.getByTestId("environment-staging")).toBeVisible();
  await page.getByTestId("nav-keys").click();
  await expect(page).toHaveURL(/\/keys/);
  await page.getByTestId("nav-segments").click();
  await expect(page).toHaveURL(/\/segments/);
  await page.getByTestId("nav-audit").click();
  await expect(page).toHaveURL(/\/audit/);
  // The audit page has no environment picker, but the nav still opens the others on staging.
  await page.getByTestId("nav-keys").click();
  await expect(page).toHaveURL(/env=staging/);
  await page.getByTestId("nav-flags").click();
  await expect(page.getByTestId("flag-checkout-v2")).toBeVisible();
});

test.describe("environments", () => {
  test("lists a project's environments", async ({ page }) => {
    await page.goto("/environments?project=shop");
    await expect(page.getByTestId("environment-production")).toContainText("Production");
    await expect(page.getByTestId("environment-staging")).toContainText("Staging");
  });

  test("creates one and shows it in the list and in the server", async ({ page, request }) => {
    await page.goto("/environments?project=shop");
    await page.getByTestId("new-environment").click();
    await page.locator("#env-name").fill("QA");
    await page.locator("#env-key").fill("qa");
    await page.getByRole("button", { name: "Create environment" }).click();
    await expect(page.getByTestId("environment-qa")).toContainText("QA");
    const answer = await request.get(`${API}/api/v1/projects/shop/environments`, { headers: AUTH });
    expect(JSON.stringify(await answer.json())).toContain('"qa"');
  });

  test("refuses a bad key before asking, and shows the server's refusal of a duplicate", async ({
    page,
  }) => {
    await page.goto("/environments?project=shop");
    await page.getByTestId("new-environment").click();
    await page.locator("#env-name").fill("Prod again");
    await page.locator("#env-key").fill("Bad Key");
    await page.getByRole("button", { name: "Create environment" }).click();
    await expect(page.getByTestId("create-error")).toContainText("Give the environment a key");

    await page.locator("#env-key").fill("production");
    await page.getByRole("button", { name: "Create environment" }).click();
    await expect(page.getByTestId("create-error")).toContainText("already exists");
  });
});

test.describe("SDK keys", () => {
  test("lists keys with their prefix, active and revoked", async ({ page }) => {
    await page.goto("/keys?project=shop&env=production");
    await expect(page.getByTestId("key-1")).toContainText("fs_srv_a1b2c3d4");
    await expect(page.getByTestId("key-1")).toContainText("Backend");
    await expect(page.getByTestId("revoke-1")).toBeVisible();
    await expect(page.getByTestId("key-2")).toContainText("revoked");
    await expect(page.getByTestId("revoke-2")).toHaveCount(0);
  });

  test("shows a new key's plaintext once: gone after leaving the page", async ({ page }) => {
    await page.goto("/keys?project=shop&env=production");
    await page.getByTestId("new-key-button").click();
    await page.locator("#key-name").fill("Worker");
    await page.getByRole("button", { name: "Create key" }).click();

    const plaintext = page.getByTestId("new-key-plaintext");
    await expect(plaintext).toContainText("SECRETPART");
    // The table has the prefix and the callout names it; the plaintext is only in the callout.
    await expect(page.getByTestId("key-3")).toContainText("fs_srv_00000001");
    await expect(page.getByTestId("key-3")).not.toContainText("SECRETPART");

    // Leave and come back: the prefix stays, the secret does not.
    await page.getByTestId("nav-segments").click();
    await expect(page).toHaveURL(/\/segments/);
    await page.getByTestId("nav-keys").click();
    await expect(page.getByTestId("key-3")).toContainText("fs_srv_00000001");
    await expect(page.getByTestId("new-key")).toHaveCount(0);
    await expect(page.locator("body")).not.toContainText("SECRETPART");

    // A reload cannot bring it back either.
    await page.reload();
    await expect(page.getByTestId("key-3")).toBeVisible();
    await expect(page.locator("body")).not.toContainText("SECRETPART");
  });

  test("dismissing the secret removes it", async ({ page }) => {
    await page.goto("/keys?project=shop&env=production");
    await page.getByTestId("new-key-button").click();
    await page.locator("#key-name").fill("Browser");
    await page.locator("#key-kind").selectOption("client");
    await page.getByRole("button", { name: "Create key" }).click();
    await expect(page.getByTestId("new-key-plaintext")).toContainText("fs_cli_");
    await page.getByTestId("dismiss-key").click();
    await expect(page.getByTestId("new-key")).toHaveCount(0);
    await expect(page.locator("body")).not.toContainText("SECRETPART");
  });

  test("needs a name to create", async ({ page }) => {
    await page.goto("/keys?project=shop&env=production");
    await page.getByTestId("new-key-button").click();
    await page.getByRole("button", { name: "Create key" }).click();
    await expect(page.getByTestId("create-error")).toBeVisible();
  });

  test("revoking asks first: cancel keeps the key, confirm revokes it", async ({
    page,
    request,
  }) => {
    await page.goto("/keys?project=shop&env=production");
    await page.getByTestId("revoke-1").click();
    await page.getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByTestId("revoke-1")).toBeVisible();

    await page.getByTestId("revoke-1").click();
    await page.getByTestId("confirm-revoke").click();
    await expect(page.getByTestId("revoke-1")).toHaveCount(0);
    await expect(page.getByTestId("key-1")).toContainText("revoked");
    const answer = await request.get(`${API}/api/v1/projects/shop/environments/production/keys`, {
      headers: AUTH,
    });
    const keys: { id: number; revokedAt: string | null }[] = await answer.json();
    expect(keys.find((k) => k.id === 1)?.revokedAt).not.toBeNull();
  });
});

test.describe("segments", () => {
  test("a version conflict keeps the typing and says to reload", async ({ page, request }) => {
    await page.goto("/segments?project=shop&env=production");
    await page.getByTestId("edit-beta-testers").click();
    await page.locator("#segment-included").fill("alice\ncarol");
    // Someone else saves after this page loaded.
    await request.post(`${API}/__bump-segment/shop/production/beta-testers`);
    await page.getByTestId("save-segment").click();

    await expect(page.getByTestId("segment-error")).toContainText("Someone else changed");
    await expect(page.getByTestId("segment-error")).toContainText("reload the page");
    await expect(page.locator("#segment-included")).toHaveValue("alice\ncarol");

    // Reloading is the way out, and what the other editor saved was not overwritten.
    await page.reload();
    await page.getByTestId("edit-beta-testers").click();
    await expect(page.locator("#segment-included")).toHaveValue("someone-else");
  });

  test("lists segments with counts", async ({ page }) => {
    await page.goto("/segments?project=shop&env=production");
    await expect(page.getByTestId("segment-beta-testers")).toContainText("2 contexts");
    await expect(page.getByTestId("segment-beta-testers")).toContainText("1 rule");
    await expect(page.getByTestId("segment-staff")).toContainText("0 rules");
  });

  test("an environment with none says so", async ({ page }) => {
    await page.goto("/segments?project=shop&env=staging");
    await expect(page.getByText("No segments yet")).toBeVisible();
  });

  test("edits members, saves and sees them after a reload", async ({ page }) => {
    await page.goto("/segments?project=shop&env=production");
    await page.getByTestId("edit-beta-testers").click();
    await expect(page.locator("#segment-included")).toHaveValue("alice\nbob");
    await page.locator("#segment-included").fill("alice\nbob\ncarol");
    await page.locator("#segment-excluded").fill("");
    await page.getByTestId("save-segment").click();
    await expect(page.getByTestId("segment-beta-testers")).toContainText("3 contexts");
    await expect(page.getByTestId("segment-beta-testers")).toContainText("0 contexts");
    await page.reload();
    await expect(page.getByTestId("segment-beta-testers")).toContainText("3 contexts");
  });

  test("shows the server's refusal of a bad rule, and a JSON error before asking", async ({
    page,
  }) => {
    await page.goto("/segments?project=shop&env=production");
    await page.getByTestId("edit-staff").click();
    await page.locator("#segment-rules").fill("not json");
    await page.getByTestId("save-segment").click();
    await expect(page.getByTestId("segment-error")).toContainText("must be a JSON array");

    await page.locator("#segment-rules").fill('[{"nope": 1}]');
    await page.getByTestId("save-segment").click();
    await expect(page.getByTestId("segment-error")).toContainText("clauses must be a list");
  });
});

test.describe("audit log", () => {
  test("lists the newest events and pages back to older ones", async ({ page }) => {
    await page.goto("/audit?project=shop");
    await expect(page.getByTestId("audit-event")).toHaveCount(25);
    await expect(page.getByTestId("audit-newest")).toHaveCount(0);
    await page.getByTestId("audit-older").click();
    await expect(page).toHaveURL(/cursor=/);
    await expect(page.getByTestId("audit-event")).toHaveCount(5);
    await expect(page.getByTestId("audit-older")).toHaveCount(0);
    await page.getByTestId("audit-newest").click();
    await expect(page.getByTestId("audit-event")).toHaveCount(25);
  });

  test("filters by flag, environment and date", async ({ page }) => {
    await page.goto("/audit?project=shop");
    await page.locator("#audit-flag").fill("checkout-v2");
    await page.getByTestId("apply-filters").click();
    await expect(page).toHaveURL(/flag=checkout-v2/);
    await expect(page.getByTestId("audit-event")).toHaveCount(10);
    await expect(page.getByTestId("audit-events")).not.toContainText("dark-mode");

    await page.locator("#audit-environment").selectOption("production");
    await page.getByTestId("apply-filters").click();
    await expect(page).toHaveURL(/environment=production/);
    await expect(page.getByTestId("audit-event")).toHaveCount(5);

    await page.locator("#audit-since").fill("2026-10-20");
    await page.getByTestId("apply-filters").click();
    await expect(page.getByTestId("audit-event")).toHaveCount(2);

    await page.getByTestId("clear-filters").click();
    await expect(page.getByTestId("audit-event")).toHaveCount(25);
  });

  test("says when nothing matches", async ({ page }) => {
    await page.goto("/audit?project=shop&flag=no-such-flag");
    await expect(page.getByText("No event matches these filters.")).toBeVisible();
  });
});
