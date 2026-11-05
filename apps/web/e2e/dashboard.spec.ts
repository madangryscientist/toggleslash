import { expect, test } from "@playwright/test";
import { API_PORT, E2E_ADMIN_TOKEN } from "./constants.ts";

const API = `http://127.0.0.1:${API_PORT}`;

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

test("lists the first project's flags in its first environment", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Flags" })).toBeVisible();
  await expect(page.getByLabel("Project")).toHaveValue("shop");
  await expect(page.getByLabel("Environment")).toHaveValue("production");
  await expect(page.getByTestId("flag-checkout-v2")).toContainText("Checkout v2");
  await expect(page.getByTestId("flag-checkout-v2")).toContainText("temporary");
  await expect(page.getByTestId("flag-checkout-v2")).toContainText("2026-11-04");
  await expect(page.getByTestId("flag-dark-mode")).toContainText("never");
  await expect(page.getByTestId("toggle-checkout-v2")).toHaveText("On");
  await expect(page.getByTestId("toggle-dark-mode")).toHaveText("Off");
});

test("switches environment and project", async ({ page }) => {
  await page.goto("/");
  await page.getByLabel("Environment").selectOption("staging");
  await expect(page).toHaveURL(/env=staging/);
  await expect(page.getByTestId("toggle-dark-mode")).toHaveText("On");

  await page.getByLabel("Project").selectOption("docs");
  await expect(page.getByLabel("Environment")).toHaveValue("live");
  await expect(page.getByTestId("flag-docs-search")).toBeVisible();
  await expect(page.getByTestId("flag-checkout-v2")).toHaveCount(0);
});

test("the last evaluated column follows the selected environment", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByTestId("flag-checkout-v2")).toContainText("2026-11-04");
  await expect(page.getByTestId("flag-search-boost")).toContainText("2026-10-30");

  await page.getByLabel("Environment").selectOption("staging");
  await expect(page).toHaveURL(/env=staging/);
  await expect(page.getByTestId("flag-checkout-v2")).toContainText("2026-11-02");
  await expect(page.getByTestId("flag-checkout-v2")).not.toContainText("2026-11-04");
  await expect(page.getByTestId("flag-search-boost")).toContainText("2026-10-28");
  await expect(page.getByTestId("flag-dark-mode")).toContainText("never");
});

test("search narrows the list by key, name and tag", async ({ page }) => {
  await page.goto("/");
  const search = page.getByLabel("Search flags");
  await search.fill("ui");
  await expect(page.getByTestId("flag-dark-mode")).toBeVisible();
  await expect(page.getByTestId("flag-search-boost")).toBeVisible();
  await expect(page.getByTestId("flag-checkout-v2")).toHaveCount(0);

  await search.fill("CHECKOUT v2");
  await expect(page.getByTestId("flag-checkout-v2")).toBeVisible();
  await expect(page.getByTestId("flag-dark-mode")).toHaveCount(0);

  await search.fill("nothing-like-this");
  await expect(page.getByText("No flags match")).toBeVisible();

  await search.fill("");
  await expect(page.getByTestId("flag-dark-mode")).toBeVisible();
});

test("toggling a flag changes it on the server and keeps it after a reload", async ({
  page,
  request,
}) => {
  await page.goto("/");
  await page.getByTestId("toggle-dark-mode").click();
  await expect(page.getByTestId("toggle-dark-mode")).toHaveText("On");

  await page.reload();
  await expect(page.getByTestId("toggle-dark-mode")).toHaveText("On");
  // The server has it, not just the page.
  const stored = await request.get(
    `${API}/api/v1/projects/shop/flags/dark-mode/environments/production`,
    { headers: { authorization: `Bearer ${E2E_ADMIN_TOKEN}` } },
  );
  expect(await stored.json()).toMatchObject({ on: true });
});

test("a stale toggle says so and shows what the server has", async ({ page, request }) => {
  await page.goto("/");
  // Someone else flips it after this page loaded.
  await request.post(`${API}/api/v1/projects/shop/flags/dark-mode/environments/production/toggle`, {
    headers: { authorization: `Bearer ${E2E_ADMIN_TOKEN}` },
    data: { on: true },
  });
  await page.getByTestId("toggle-dark-mode").click();
  await expect(page.getByTestId("problem")).toContainText("changed");
  await expect(page.getByTestId("toggle-dark-mode")).toHaveText("On");
});

test("creates a flag", async ({ page }) => {
  await page.goto("/");
  await page.getByTestId("new-flag").click();
  await page.getByLabel("Name").fill("Fast lane");
  await page.getByLabel("Key").fill("fast-lane");
  await page.getByLabel("Tags").fill("perf, checkout");
  await page.getByLabel("Temporary").check();
  await page.getByRole("button", { name: "Create flag" }).click();

  const row = page.getByTestId("flag-fast-lane");
  await expect(row).toBeVisible();
  await expect(row).toContainText("Fast lane");
  await expect(row).toContainText("temporary");
  await expect(row).toContainText("perf");
  await expect(page.getByTestId("toggle-fast-lane")).toHaveText("Off");
});

test("creating a flag with a taken key shows the server's reason and keeps the dialog", async ({
  page,
}) => {
  await page.goto("/");
  await page.getByTestId("new-flag").click();
  await page.getByLabel("Name").fill("Again");
  await page.getByLabel("Key").fill("dark-mode");
  await page.getByRole("button", { name: "Create flag" }).click();
  await expect(page.getByTestId("create-error")).toContainText("already exists");
});

test("a bad key is caught before it is sent", async ({ page }) => {
  await page.goto("/");
  await page.getByTestId("new-flag").click();
  await page.getByLabel("Name").fill("Bad");
  await page.getByLabel("Key").fill("Not A Key");
  await page.getByRole("button", { name: "Create flag" }).click();
  await expect(page.getByTestId("create-error")).toContainText("key");
});

test("archives a flag after confirming", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Archive Dark mode" }).click();
  // Cancelling leaves it.
  await page.getByRole("button", { name: "Cancel" }).click();
  await expect(page.getByTestId("flag-dark-mode")).toBeVisible();

  await page.getByRole("button", { name: "Archive Dark mode" }).click();
  await page.getByTestId("confirm-archive").click();
  await expect(page.getByTestId("flag-dark-mode")).toHaveCount(0);
  await expect(page.getByTestId("flag-checkout-v2")).toBeVisible();

  await page.reload();
  await expect(page.getByTestId("flag-dark-mode")).toHaveCount(0);
});

test("shows a page, not a crash, when the server is down", async ({ page, request }) => {
  await request.post(`${API}/__down`);
  const response = await page.goto("/");
  expect(response?.status()).toBeLessThan(500);
  const down = page.getByTestId("server-down");
  await expect(down).toContainText("not reachable");
  await expect(page.getByRole("button", { name: /try again|retry/i })).toBeVisible();

  // It comes back without a restart.
  await request.post(`${API}/__reset`);
  await page.getByRole("button", { name: /try again|retry/i }).click();
  await expect(page.getByTestId("flag-checkout-v2")).toBeVisible();
});

test("the admin token is not in the page or any script the browser loads", async ({ page }) => {
  const bodies: string[] = [];
  page.on("response", async (response) => {
    const type = response.headers()["content-type"] ?? "";
    if (/javascript|json|html|css/.test(type)) {
      bodies.push(await response.text().catch(() => ""));
    }
  });
  await page.goto("/");
  await page.getByTestId("toggle-dark-mode").click();
  await expect(page.getByTestId("toggle-dark-mode")).toHaveText("On");
  await page.waitForLoadState("networkidle");
  bodies.push(await page.content());

  expect(bodies.length).toBeGreaterThan(2);
  for (const body of bodies) {
    expect(body).not.toContain(E2E_ADMIN_TOKEN);
    // The client never builds the Authorization header or reads the Worker's env.
    expect(body).not.toContain("Bearer ");
    expect(body).not.toContain("cloudflare:workers");
  }
});
