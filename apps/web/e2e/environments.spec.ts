import { expect, test, type Page } from "@playwright/test";
import { API_PORT } from "./constants.ts";
import { gotoHydrated } from "./goto.ts";

test.beforeEach(async ({ request }) => {
  await request.post(`http://127.0.0.1:${API_PORT}/__reset`);
});

const environmentRow = (page: Page, name: string) =>
  page.getByRole("row", { name: new RegExp(`^${name} `) });

test("renaming keeps the key, shows the new name, and survives a reload", async ({ page }) => {
  await gotoHydrated(page, "/environments?project=shop");
  await page.getByRole("button", { name: "Rename staging" }).click();
  const dialog = page.getByRole("dialog", { name: "Rename Staging" });
  await dialog.getByLabel("Name").fill("Pre-production");
  await dialog.getByRole("button", { name: "Rename", exact: true }).click();

  const row = environmentRow(page, "Pre-production");
  await expect(row).toContainText("staging");
  await page.reload();
  await expect(environmentRow(page, "Pre-production")).toContainText("staging");
});

test("Delete, once confirmed, removes the environment", async ({ page }) => {
  await gotoHydrated(page, "/environments?project=shop");
  await page.getByRole("button", { name: "Delete staging" }).click();
  await page
    .getByRole("dialog", { name: "Delete staging?" })
    .getByRole("button", { name: "Delete" })
    .click();
  await expect(environmentRow(page, "Staging")).toHaveCount(0);
  await page.reload();
  await expect(environmentRow(page, "Staging")).toHaveCount(0);
  await expect(environmentRow(page, "Production")).toBeVisible();
});

test.describe("secure mode and the signing key", () => {
  test("a new key is shown once: gone after closing and after a reload", async ({ page }) => {
    await gotoHydrated(page, "/environments?project=shop");
    await page.getByRole("button", { name: "Signing key for staging" }).click();
    await page
      .getByRole("dialog", { name: "Make a signing key for Staging?" })
      .getByRole("button", { name: "Make key" })
      .click();
    const dialog = page.getByRole("dialog", { name: "Signing key for Staging" });
    const pem = dialog.getByRole("figure", { name: "PEM" });
    await expect(pem).toContainText("-----BEGIN PRIVATE KEY-----");
    await expect(pem.getByRole("button", { name: "copy" })).toBeVisible();
    const body = (await pem.textContent()) ?? "";
    // The PEM's base64 body, without its armour: what must not survive the dialog.
    const secret = (
      body.split("\n").find((line) => /^[A-Za-z0-9+/=]{40,}$/.test(line.trim())) ?? ""
    ).trim();
    expect(secret.length).toBeGreaterThan(40);
    await expect(environmentRow(page, "Staging")).toContainText(/Made \d{4}-\d{2}-\d{2}/);

    await dialog.getByRole("button", { name: "I have copied it" }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    expect(await page.content()).not.toContain(secret);
    await page.reload();
    await expect(environmentRow(page, "Staging")).toContainText(/Made /);
    expect(await page.content()).not.toContain(secret);
    expect(await page.content()).not.toContain("BEGIN PRIVATE KEY");
  });

  test("with a key made, the switch turns secure mode on, and that survives a reload", async ({
    page,
  }) => {
    await gotoHydrated(page, "/environments?project=shop");
    const toggle = page.getByRole("switch", { name: "Secure mode for staging" });
    await expect(toggle).toBeDisabled();

    await page.getByRole("button", { name: "Signing key for staging" }).click();
    await page
      .getByRole("dialog", { name: "Make a signing key for Staging?" })
      .getByRole("button", { name: "Make key" })
      .click();
    await page
      .getByRole("dialog", { name: "Signing key for Staging" })
      .getByRole("button", { name: "I have copied it" })
      .click();
    await expect(toggle).toBeEnabled();

    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-checked", "true");
    await page.reload();
    await expect(toggle).toHaveAttribute("aria-checked", "true");
    await expect(page.getByRole("switch", { name: "Secure mode for production" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
  });
});

// A real browser test: Escape on a native <dialog> is the browser's, and happy-dom binds no key to it.
test.describe("a signing key on a slow server", () => {
  test("Escape does not close the confirm while the key is being made, so the new key is not lost", async ({
    page,
  }) => {
    await gotoHydrated(page, "/environments?project=shop");
    await page.getByRole("button", { name: "Signing key for staging" }).click();
    // Hold the dashboard's server call, as a slow server would, until Escape has been pressed.
    let release: (() => void) | undefined;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route("**/_serverFn/**", async (route) => {
      if (route.request().method() === "POST") await held;
      await route.continue();
    });
    const confirm = page.getByRole("dialog", { name: "Make a signing key for Staging?" });
    await confirm.getByRole("button", { name: "Make key" }).click();
    await expect(confirm.getByRole("button", { name: "Cancel" })).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(confirm).toBeVisible();
    release?.();
    await expect(page.getByRole("dialog", { name: "Signing key for Staging" })).toContainText(
      "BEGIN PRIVATE KEY",
    );
  });
});
