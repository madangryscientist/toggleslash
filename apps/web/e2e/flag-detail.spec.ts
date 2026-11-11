import { expect, test, type Page } from "@playwright/test";
import { API_PORT, E2E_ADMIN_TOKEN } from "./constants.ts";
import { waitForHydrated } from "./hydrated.ts";

const API = `http://127.0.0.1:${API_PORT}`;
const CONFIG = `${API}/api/v1/projects/shop/flags/checkout-v2/environments/production`;
const AUTH = { authorization: `Bearer ${E2E_ADMIN_TOKEN}` };

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

async function openCheckout(page: Page) {
  await page.goto("/flags/checkout-v2?project=shop&env=production");
  await expect(page.getByRole("heading", { name: "Checkout v2" })).toBeVisible();
  // The heading and the server-rendered fields show before React hydrates; a fill that lands first
  // merges with the value React then restores, so wait for the editor to be live and loaded.
  await waitForHydrated(page, '[data-testid="targets-0"]');
  await expect(page.getByTestId("targets-0")).toHaveValue("alice\nbob");
}

test("opens from the flag list and shows the flag's targeting", async ({ page }) => {
  await page.goto("/");
  await page.getByTestId("open-checkout-v2").click();
  await expect(page).toHaveURL(/\/flags\/checkout-v2/);
  await expect(page.getByTestId("meta-name")).toHaveValue("Checkout v2");
  await expect(page.getByTestId("targeting-on")).toBeChecked();
  await expect(page.getByTestId("targets-0")).toHaveValue(/alice/);
  await expect(page.getByTestId("rule-0-clause-0-attribute")).toHaveValue("country");
  await expect(page.getByTestId("rule-0-percent-0")).toHaveValue("25");
  await expect(page.getByTestId("rule-0-percent-1")).toHaveValue("75");
  await expect(page.getByText("Version 3", { exact: true })).toBeVisible();
});

test("edits a rule and a rollout, saves, and sees them after a reload", async ({
  page,
  request,
}) => {
  await openCheckout(page);
  await expect(page.getByTestId("save-targeting")).toBeDisabled();

  await page.getByTestId("rule-0-clause-0-values").fill("NL, DE, BE");
  await page.getByTestId("rule-0-percent-0").fill("40");
  await page.getByTestId("rule-0-percent-1").fill("60");
  await expect(page.getByTestId("rule-0-total")).toContainText("100%");
  await page.getByTestId("save-targeting").click();

  await expect(page.getByTestId("notice")).toContainText("Targeting saved");
  await expect(page.getByText("Version 4", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByTestId("rule-0-clause-0-values")).toHaveValue(/BE/);
  await expect(page.getByTestId("rule-0-percent-0")).toHaveValue("40");
  await expect(page.getByTestId("rule-0-percent-1")).toHaveValue("60");

  // The server has it, not just the page.
  const stored = await request.get(CONFIG, { headers: AUTH });
  expect(await stored.json()).toMatchObject({
    version: 4,
    rules: [
      {
        clauses: [{ op: "in", attribute: "country", values: ["NL", "DE", "BE"] }],
        rollout: {
          variations: [
            { variation: 0, weight: 40000 },
            { variation: 1, weight: 60000 },
          ],
        },
      },
    ],
  });
});

test("a semver operator is picked from the menu and a bare version is saved as a string", async ({
  page,
  request,
}) => {
  await openCheckout(page);

  await page.getByTestId("rule-0-clause-0-op").selectOption("semverGte");
  await page.getByTestId("rule-0-clause-0-values").fill("2.1");
  await page.getByTestId("save-targeting").click();

  await expect(page.getByTestId("notice")).toContainText("Targeting saved");
  await page.reload();
  await expect(page.getByTestId("rule-0-clause-0-op")).toHaveValue("semverGte");
  await expect(page.getByTestId("rule-0-clause-0-values")).toHaveValue('"2.1"');
  const stored = await request.get(CONFIG, { headers: AUTH });
  expect(await stored.json()).toMatchObject({
    rules: [{ clauses: [{ op: "semverGte", values: ["2.1"] }] }],
  });
});

test("a matches operator is picked from the menu and a pattern with a comma is saved as a string", async ({
  page,
  request,
}) => {
  await openCheckout(page);

  await page.getByTestId("rule-0-clause-0-op").selectOption("matches");
  await page.getByTestId("rule-0-clause-0-values").fill('\\d+, "a{1,3}"');
  await page.getByTestId("save-targeting").click();

  await expect(page.getByTestId("notice")).toContainText("Targeting saved");
  await page.reload();
  await expect(page.getByTestId("rule-0-clause-0-op")).toHaveValue("matches");
  await expect(page.getByTestId("rule-0-clause-0-values")).toHaveValue('\\d+, "a{1,3}"');
  const stored = await request.get(CONFIG, { headers: AUTH });
  expect(await stored.json()).toMatchObject({
    rules: [{ clauses: [{ op: "matches", values: ["\\d+", "a{1,3}"] }] }],
  });
});

test("adds a rule and a prerequisite and turns targeting off", async ({ page, request }) => {
  await openCheckout(page);
  await page.getByTestId("add-rule").click();
  await page.getByTestId("rule-1-clause-0-attribute").fill("plan");
  await page.getByTestId("rule-1-clause-0-values").fill("pro");
  await page.getByTestId("rule-1-variation").selectOption("0");
  await page.getByTestId("add-prerequisite").click();
  await page.getByTestId("prerequisite-0-flag").selectOption("dark-mode");
  await page.getByTestId("targeting-on").uncheck();
  await page.getByTestId("save-targeting").click();
  await expect(page.getByTestId("notice")).toContainText("Targeting saved");

  const stored = await (await request.get(CONFIG, { headers: AUTH })).json();
  expect(stored).toMatchObject({
    on: false,
    prerequisites: [{ flagKey: "dark-mode", variation: 0 }],
  });
  expect(stored.rules).toHaveLength(2);
});

test("a rollout that does not total 100% is shown at the rule, and the draft is kept", async ({
  page,
}) => {
  await openCheckout(page);
  await page.getByTestId("rule-0-percent-0").fill("30");
  await expect(page.getByTestId("rule-0-total")).toContainText("105%");
  await page.getByTestId("save-targeting").click();

  await expect(page.getByTestId("rule-0")).toContainText("weights must sum to 100000");
  await expect(page.getByTestId("config-error")).toBeVisible();
  await expect(page.getByTestId("rule-0-percent-0")).toHaveValue("30");

  // Fixing it clears the message.
  await page.getByTestId("rule-0-percent-1").fill("70");
  await expect(page.getByTestId("config-error")).toHaveCount(0);
  await page.getByTestId("save-targeting").click();
  await expect(page.getByTestId("notice")).toContainText("Targeting saved");
});

test("a clause with no attribute is refused by the server at that clause", async ({ page }) => {
  await openCheckout(page);
  await page.getByTestId("rule-0-clause-0-attribute").fill("");
  await page.getByTestId("save-targeting").click();
  await expect(page.getByTestId("rule-0")).toContainText("attribute must be a non-empty string");
});

test("a percentage that is not a number is caught before it is sent", async ({ page }) => {
  await openCheckout(page);
  await page.getByTestId("rule-0-percent-0").fill("lots");
  await page.getByTestId("save-targeting").click();
  await expect(page.getByTestId("rule-0")).toContainText("A percentage from 0 to 100");
  await expect(page.getByTestId("config-error")).toBeVisible();
});

test("a version conflict keeps the typing and says to reload", async ({ page, request }) => {
  await openCheckout(page);
  await page.getByTestId("rule-0-clause-0-values").fill("NL, DE, BE");
  // Someone else saves after this page loaded.
  await request.post(`${API}/__bump/shop/production/checkout-v2`);
  await page.getByTestId("save-targeting").click();

  const conflict = page.getByTestId("config-conflict");
  await expect(conflict).toContainText("Someone else changed this flag");
  await expect(conflict).toContainText("reload the page");
  await expect(page.getByTestId("rule-0-clause-0-values")).toHaveValue("NL, DE, BE");
  await expect(page.getByText("Version 3", { exact: true })).toBeVisible();

  // More typing does not hide the warning, and a details save reloads the page's data without
  // replacing the editor: the typing stays, and saving it again still refuses.
  await page.getByTestId("rule-0-clause-0-values").fill("NL, DE, BE, FR");
  await expect(conflict).toBeVisible();
  await page.getByTestId("meta-name").fill("Checkout renamed");
  await page.getByTestId("save-meta").click();
  await expect(page.getByTestId("notice")).toContainText("details were saved");
  await expect(page.getByTestId("rule-0-clause-0-values")).toHaveValue("NL, DE, BE, FR");
  await page.getByTestId("save-targeting").click();
  await expect(conflict).toBeVisible();
  await expect(page.getByTestId("rule-0-clause-0-values")).toHaveValue("NL, DE, BE, FR");

  // Reloading is the way out: the latest version, the typing gone, the warning gone.
  await page.reload();
  await expect(page.getByText("Version 4", { exact: true })).toBeVisible();
  await expect(page.getByTestId("rule-0-clause-0-values")).not.toHaveValue(/BE/);
  await expect(conflict).toBeHidden();
});

test("edits the flag's details and sees a field error from the server", async ({ page }) => {
  await openCheckout(page);
  await page.getByTestId("meta-name").fill("");
  await page.getByTestId("save-meta").click();
  await expect(page.getByTestId("meta-form")).toContainText("name must not be empty");

  await page.getByTestId("meta-name").fill("Checkout v2 (new)");
  await page.getByTestId("meta-owner").fill("payments");
  await page.getByTestId("meta-tags").fill("checkout, money");
  await page.getByTestId("save-meta").click();
  await expect(page.getByTestId("notice")).toContainText("Details saved");

  await page.reload();
  await expect(page.getByTestId("meta-name")).toHaveValue("Checkout v2 (new)");
  await expect(page.getByTestId("meta-owner")).toHaveValue("payments");
  await expect(page.getByTestId("meta-tags")).toHaveValue("checkout, money");
});

test("shows the flag's history and usage", async ({ page }) => {
  await openCheckout(page);
  await expect(page.getByTestId("history-event")).toHaveCount(3);
  await expect(page.getByTestId("history")).toContainText("2026-11-04 10:00:00 UTC");
  await expect(page.getByTestId("usage-total-production")).toContainText("42");
  await expect(page.getByTestId("usage-production")).toContainText("2026-11-04");
  await expect(page.getByTestId("usage-production")).toContainText("true: 10, false: 20");
});

test("a save shows up in the history", async ({ page }) => {
  await openCheckout(page);
  await page.getByTestId("targeting-on").uncheck();
  await page.getByTestId("save-targeting").click();
  await expect(page.getByTestId("history-event")).toHaveCount(4);
});

test("an unknown flag says so instead of crashing", async ({ page }) => {
  const response = await page.goto("/flags/nope?project=shop&env=production");
  expect(response?.status()).toBeLessThan(500);
  await expect(page.getByTestId("server-down")).toBeVisible();
});

test("the admin token is not in the detail page", async ({ page }) => {
  await openCheckout(page);
  await page.waitForLoadState("networkidle");
  expect(await page.content()).not.toContain(E2E_ADMIN_TOKEN);
});

test.describe("context kinds", () => {
  test("an org target and an org clause are added, saved, and shown after a reload", async ({
    page,
    request,
  }) => {
    await openCheckout(page);
    await page.getByTestId("add-other-target").click();
    await page.getByTestId("other-target-1-variation").selectOption("1");
    await page.getByTestId("other-target-1-kind").fill("org");
    await page.getByTestId("other-target-1-keys").fill("acme\nglobex");
    await page.getByTestId("rule-0-clause-0-kind").fill("org");
    await page.getByTestId("rule-0-rollout-kind").fill("org");
    await page.getByTestId("save-targeting").click();
    await expect(page.getByTestId("notice")).toContainText("Targeting saved");

    await page.reload();
    await expect(page.getByTestId("other-target-1-kind")).toHaveValue("org");
    await expect(page.getByTestId("other-target-1-variation")).toHaveValue("1");
    await expect(page.getByTestId("other-target-1-keys")).toHaveValue("acme\nglobex");
    await expect(page.getByTestId("rule-0-clause-0-kind")).toHaveValue("org");
    await expect(page.getByTestId("rule-0-rollout-kind")).toHaveValue("org");
    // The user target box is still only the user target.
    await expect(page.getByTestId("targets-0")).toHaveValue("alice\nbob");

    const stored = await (await request.get(CONFIG, { headers: AUTH })).json();
    expect(stored.targets).toEqual([
      { variation: 0, contextKeys: ["alice", "bob"] },
      { variation: 1, contextKeys: ["acme", "globex"], contextKind: "org" },
    ]);
    expect(stored.rules[0].clauses[0].contextKind).toBe("org");
    expect(stored.rules[0].rollout.contextKind).toBe("org");
  });
});

test.describe("a flag that already holds context kinds", () => {
  test("keeps them through an unrelated edit", async ({ page, request }) => {
    const before = await (await request.get(CONFIG, { headers: AUTH })).json();
    const seeded = {
      ...before,
      targets: [
        { variation: 1, contextKeys: ["acme"], contextKind: "org" },
        { variation: 0, contextKeys: ["alice", "bob"] },
      ],
      rules: [
        {
          clauses: [{ op: "in", attribute: "plan", values: ["pro"], contextKind: "org" }],
          rollout: {
            variations: [
              { variation: 0, weight: 25_000 },
              { variation: 1, weight: 75_000 },
            ],
            contextKind: "org",
          },
        },
      ],
    };
    const put = await request.put(CONFIG, { headers: AUTH, data: seeded });
    expect(put.ok()).toBe(true);

    await openCheckout(page);
    await page.getByTestId("targets-0").fill("alice\nbob\ncarol");
    await page.getByTestId("save-targeting").click();
    await expect(page.getByTestId("notice")).toContainText("Targeting saved");

    const stored = await (await request.get(CONFIG, { headers: AUTH })).json();
    expect(stored.targets).toEqual([
      { variation: 1, contextKeys: ["acme"], contextKind: "org" },
      { variation: 0, contextKeys: ["alice", "bob", "carol"] },
    ]);
    expect(stored.rules[0].clauses[0].contextKind).toBe("org");
    expect(stored.rules[0].rollout.contextKind).toBe("org");
  });

  test("an invalid kind shows the problem and nothing is sent", async ({ page, request }) => {
    await openCheckout(page);
    await page.getByTestId("add-other-target").click();
    await page.getByTestId("other-target-1-kind").fill("not valid");
    await page.getByTestId("other-target-1-keys").fill("acme");
    await page.getByTestId("rule-0-clause-0-kind").fill("multi");
    await page.getByTestId("rule-0-rollout-kind").fill("a/b");
    await page.getByTestId("save-targeting").click();

    await expect(page.getByTestId("config-error")).toBeVisible();
    // One message each: the other-kind target, the clause and the rollout.
    await expect(page.getByText("A context kind uses letters")).toHaveCount(3);
    await expect(page.getByTestId("notice")).toHaveCount(0);
    const stored = await (await request.get(CONFIG, { headers: AUTH })).json();
    expect(stored.version).toBe(3);
  });
});
