import { expect, test, type APIRequestContext } from "@playwright/test";
import { API_PORT, E2E_ADMIN_TOKEN } from "./constants.ts";
import { gotoHydrated } from "./goto.ts";

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
    await gotoHydrated(page, "/environments?project=shop");
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
    await gotoHydrated(page, "/environments?project=shop");
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
    await gotoHydrated(page, "/environments?project=shop");
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
    await gotoHydrated(page, "/environments?project=shop");
    await page.getByTestId("delete-production").click();
    await page.getByTestId("confirm-delete-environment").click();
    await expect(page.getByTestId("problem")).toContainText('Environment "production" still has');
    await expect(page.getByTestId("problem")).toContainText("Revoke them first.");
    await expect(page.getByTestId("environment-production")).toBeVisible();
  });
});

test.describe("secure mode and the signing key", () => {
  test("Cancel on the confirm makes no key", async ({ page }) => {
    await gotoHydrated(page, "/environments?project=shop");
    await page.getByTestId("signing-key-staging").click();
    await expect(
      page.getByRole("dialog", { name: "Make a signing key for Staging?" }),
    ).toContainText("every signature made with the old key stops working");
    await page.getByRole("button", { name: "Cancel" }).click();
    await expect(page.getByTestId("signing-key-date-staging")).toHaveText("No signing key");
    await page.reload();
    await expect(page.getByTestId("signing-key-date-staging")).toHaveText("No signing key");
  });

  test("a new key is shown once: gone after closing and after a reload", async ({ page }) => {
    await gotoHydrated(page, "/environments?project=shop");
    await page.getByTestId("signing-key-staging").click();
    await page.getByTestId("confirm-signing-key").click();
    const pem = page.getByTestId("signing-key-pem");
    await expect(pem).toContainText("-----BEGIN PRIVATE KEY-----");
    await expect(pem.getByRole("button", { name: "copy" })).toBeVisible();
    const body = (await pem.textContent()) ?? "";
    // The PEM's base64 body, without its armour: what must not survive the dialog.
    const secret = (
      body.split("\n").find((line) => /^[A-Za-z0-9+/=]{40,}$/.test(line.trim())) ?? ""
    ).trim();
    expect(secret.length).toBeGreaterThan(40);
    await expect(page.getByTestId("signing-key-date-staging")).toHaveText(
      /^Made \d{4}-\d{2}-\d{2}$/,
    );

    await page.getByTestId("close-signing-key").click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    expect(await page.content()).not.toContain(secret);
    await page.reload();
    await expect(page.getByTestId("signing-key-date-staging")).toHaveText(/^Made /);
    expect(await page.content()).not.toContain(secret);
    expect(await page.content()).not.toContain("BEGIN PRIVATE KEY");
  });

  test("the switch waits for a key, then turns secure mode on, and that survives a reload", async ({
    page,
  }) => {
    await gotoHydrated(page, "/environments?project=shop");
    const toggle = page.getByTestId("secure-mode-staging");
    await expect(toggle).toBeDisabled();
    await expect(page.getByTestId("environment-staging")).toContainText(
      "Make a signing key first.",
    );
    await expect(toggle).toHaveAttribute("aria-checked", "false");

    await page.getByTestId("signing-key-staging").click();
    await page.getByTestId("confirm-signing-key").click();
    await expect(page.getByTestId("signing-key-pem")).toBeVisible();
    await page.getByTestId("close-signing-key").click();
    await expect(toggle).toBeEnabled();
    await expect(page.getByTestId("environment-staging")).not.toContainText(
      "Make a signing key first.",
    );

    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-checked", "true");
    await expect(page.getByTestId("problem")).toHaveCount(0);
    await page.reload();
    await expect(page.getByTestId("secure-mode-staging")).toHaveAttribute("aria-checked", "true");
    await expect(page.getByTestId("secure-mode-production")).toHaveAttribute(
      "aria-checked",
      "false",
    );
  });
});

test.describe("a signing key on a slow server", () => {
  test("the confirm cannot be closed while the key is being made, so the new key is not lost", async ({
    page,
  }) => {
    await gotoHydrated(page, "/environments?project=shop");
    await page.getByTestId("signing-key-staging").click();
    // Hold the dashboard's server call, as a slow server would, until the closes below have been tried.
    let release: (() => void) | undefined;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route("**/_serverFn/**", async (route) => {
      if (route.request().method() === "POST") await held;
      await route.continue();
    });
    await page.getByTestId("confirm-signing-key").click();
    const dialog = page.getByRole("dialog", { name: "Make a signing key for Staging?" });
    await expect(dialog.getByRole("button", { name: "Cancel" })).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(dialog).toBeVisible();
    release?.();
    await expect(page.getByTestId("signing-key-pem")).toContainText("BEGIN PRIVATE KEY");
  });
});
