import { expect, test, type Page } from "@playwright/test";
import { API_PORT } from "./constants.ts";
import { gotoHydrated } from "./goto.ts";

/** A section link in the dashboard nav. The nav is also in a phone menu, hidden on desktop, so match by role. */
const section = (page: Page, label: string) =>
  page
    .getByRole("navigation", { name: "Sections" })
    .getByRole("link", { name: label, exact: true });

const API = `http://127.0.0.1:${API_PORT}`;

/** The audit log's events, each an item of its list. */
const events = (page: Page) => page.getByRole("list", { name: "Audit log" }).getByRole("listitem");

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

test("creates an environment, and it is listed after a reload", async ({ page }) => {
  await gotoHydrated(page, "/environments?project=shop");
  await page.getByRole("button", { name: "New environment" }).click();
  const dialog = page.getByRole("dialog", { name: "New environment" });
  await dialog.getByLabel("Name").fill("QA");
  await dialog.getByLabel("Key", { exact: true }).fill("qa");
  await dialog.getByRole("button", { name: "Create environment" }).click();
  await expect(page.getByRole("row", { name: /^QA qa/ })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("row", { name: /^QA qa/ })).toBeVisible();
});

test.describe("SDK keys", () => {
  test("shows a new key's plaintext once: gone after leaving the page", async ({ page }) => {
    await gotoHydrated(page, "/keys?project=shop&env=production");
    await page.getByRole("button", { name: "New key" }).click();
    const dialog = page.getByRole("dialog", { name: "New SDK key" });
    await dialog.getByLabel("Name").fill("Worker");
    await dialog.getByRole("button", { name: "Create key" }).click();

    // The plaintext's line has no name of its own: only the callout around it does.
    const plaintext = page.getByTestId("new-key-plaintext");
    await expect(plaintext).toContainText("SECRETPART");
    // The table has the prefix and the callout names it; the plaintext is only in the callout.
    const row = page.getByRole("row", { name: /^Worker/ });
    await expect(row).toContainText("tgl_srv_00000001");
    await expect(row).not.toContainText("SECRETPART");

    // Leave and come back: the prefix stays, the secret does not.
    await section(page, "Segments").click();
    await expect(page.getByRole("heading", { name: "Segments" })).toBeVisible();
    await section(page, "SDK keys").click();
    await expect(page.getByRole("row", { name: /^Worker/ })).toContainText("tgl_srv_00000001");
    await expect(page.getByText("SECRETPART")).toHaveCount(0);

    // A reload cannot bring it back either.
    await page.reload();
    await expect(page.getByRole("row", { name: /^Worker/ })).toBeVisible();
    await expect(page.getByText("SECRETPART")).toHaveCount(0);
  });

  test("revoking, once confirmed, revokes the key", async ({ page }) => {
    await gotoHydrated(page, "/keys?project=shop&env=production");
    await page.getByRole("button", { name: "Revoke Backend" }).click();
    await page
      .getByRole("dialog", { name: "Revoke Backend?" })
      .getByRole("button", { name: "Revoke" })
      .click();
    await expect(page.getByRole("button", { name: "Revoke Backend" })).toHaveCount(0);
    await expect(page.getByRole("row", { name: /^Backend/ })).toContainText("revoked");
    await page.reload();
    await expect(page.getByRole("row", { name: /^Backend/ })).toContainText("revoked");
  });
});

test("edits a segment's members, saves and sees them after a reload", async ({ page }) => {
  await gotoHydrated(page, "/segments?project=shop&env=production");
  await page.getByRole("button", { name: "Edit beta-testers" }).click();
  const dialog = page.getByRole("dialog", { name: "Edit beta-testers" });
  await expect(dialog.getByLabel("Always included")).toHaveValue("alice\nbob");
  await dialog.getByLabel("Always included").fill("alice\nbob\ncarol");
  await dialog.getByLabel("Always excluded").fill("");
  await dialog.getByRole("button", { name: "Save segment" }).click();
  const row = page.getByRole("row", { name: /^beta-testers/ });
  await expect(row).toContainText("3 contexts");
  await expect(row).toContainText("0 contexts");
  await page.reload();
  await expect(page.getByRole("row", { name: /^beta-testers/ })).toContainText("3 contexts");
});

test.describe("audit log", () => {
  test("lists the newest events and pages back to older ones", async ({ page }) => {
    await gotoHydrated(page, "/audit?project=shop");
    await expect(events(page)).toHaveCount(25);
    await expect(page.getByRole("link", { name: "Back to the newest" })).toHaveCount(0);
    await page.getByRole("link", { name: "Older events" }).click();
    await expect(page).toHaveURL(/cursor=/);
    await expect(events(page)).toHaveCount(5);
    await expect(page.getByRole("link", { name: "Older events" })).toHaveCount(0);
    await page.getByRole("link", { name: "Back to the newest" }).click();
    await expect(events(page)).toHaveCount(25);
  });

  test("filters by flag, environment and date", async ({ page }) => {
    await gotoHydrated(page, "/audit?project=shop");
    const filters = page.getByRole("form", { name: "Filter the audit log" });
    const apply = filters.getByRole("button", { name: "Apply filters" });
    await filters.getByLabel("Flag").fill("checkout-v2");
    await apply.click();
    await expect(page).toHaveURL(/flag=checkout-v2/);
    await expect(events(page)).toHaveCount(10);
    await expect(page.getByRole("list", { name: "Audit log" })).not.toContainText("dark-mode");

    await filters.getByLabel("Environment").selectOption("production");
    await apply.click();
    await expect(page).toHaveURL(/environment=production/);
    await expect(events(page)).toHaveCount(5);

    await filters.getByLabel("Since").fill("2026-10-20");
    await apply.click();
    await expect(events(page)).toHaveCount(2);

    await filters.getByRole("button", { name: "Clear" }).click();
    await expect(events(page)).toHaveCount(25);
  });
});
