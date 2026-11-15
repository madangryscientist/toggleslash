import { expect, test, type Page } from "@playwright/test";
import { API_PORT } from "./constants.ts";
import { gotoHydrated } from "./goto.ts";

const API = `http://127.0.0.1:${API_PORT}`;

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

/** A rule's card in the targeting editor, by its heading. */
const rule = (page: Page, n: number) => page.getByRole("region", { name: `Rule ${n}` });
const save = (page: Page) => page.getByRole("button", { name: "Save targeting" });
/** The notice a save shows once the page has its data again. */
const notice = (page: Page) => page.getByRole("status");

async function openCheckout(page: Page) {
  await gotoHydrated(page, "/flags/checkout-v2?project=shop&env=production");
  await expect(page.getByRole("heading", { name: "Checkout v2", level: 1 })).toBeVisible();
  await expect(page.getByLabel("Serve 0: true")).toHaveValue("alice\nbob");
}

test("opens from the flag list and shows the flag's targeting", async ({ page }) => {
  await gotoHydrated(page, "/");
  await page.getByRole("link", { name: "Checkout v2" }).click();
  await expect(page).toHaveURL(/\/flags\/checkout-v2/);
  await expect(page.getByLabel("Name")).toHaveValue("Checkout v2");
  await expect(page.getByRole("checkbox", { name: /Targeting is on/ })).toBeChecked();
  await expect(page.getByLabel("Serve 0: true")).toHaveValue(/alice/);
  await expect(rule(page, 1).getByLabel("Attribute")).toHaveValue("country");
  await expect(rule(page, 1).getByLabel("true (%)")).toHaveValue("25");
  await expect(rule(page, 1).getByLabel("false (%)")).toHaveValue("75");
  await expect(page.getByText("Version 3", { exact: true })).toBeVisible();
});

test("edits a rule and a rollout, saves, and sees them after a reload and in the history", async ({
  page,
}) => {
  await openCheckout(page);
  await expect(save(page)).toBeDisabled();
  const history = page.getByRole("list", { name: "History of this flag" }).getByRole("listitem");
  await expect(history).toHaveCount(3);

  await rule(page, 1).getByLabel("Values").fill("NL, DE, BE");
  await rule(page, 1).getByLabel("true (%)").fill("40");
  await rule(page, 1).getByLabel("false (%)").fill("60");
  await expect(rule(page, 1)).toContainText("100%");
  await save(page).click();

  await expect(notice(page)).toContainText("Targeting saved");
  await expect(page.getByText("Version 4", { exact: true })).toBeVisible();
  await expect(history).toHaveCount(4);
  await page.reload();
  await expect(rule(page, 1).getByLabel("Values")).toHaveValue(/BE/);
  await expect(rule(page, 1).getByLabel("true (%)")).toHaveValue("40");
  await expect(rule(page, 1).getByLabel("false (%)")).toHaveValue("60");
});

test("adds a rule and a prerequisite and turns targeting off", async ({ page }) => {
  await openCheckout(page);
  await page.getByRole("button", { name: "Add rule" }).click();
  await rule(page, 2).getByLabel("Attribute").fill("plan");
  await rule(page, 2).getByLabel("Values").fill("pro");
  await rule(page, 2).getByLabel("Variation").selectOption("0");
  await page.getByRole("button", { name: "Add prerequisite" }).click();
  const prerequisites = page.getByRole("region", { name: "Prerequisites" });
  await prerequisites.getByLabel("Prerequisite 1", { exact: true }).selectOption("dark-mode");
  await page.getByRole("checkbox", { name: /Targeting is on/ }).uncheck();
  await save(page).click();
  await expect(notice(page)).toContainText("Targeting saved");

  await page.reload();
  await expect(page.getByRole("checkbox", { name: /Targeting is on/ })).not.toBeChecked();
  await expect(prerequisites.getByLabel("Prerequisite 1", { exact: true })).toHaveValue(
    "dark-mode",
  );
  await expect(rule(page, 2).getByLabel("Values")).toHaveValue("pro");
});

test("edits the flag's details, and sees them after a reload", async ({ page }) => {
  await openCheckout(page);
  await page.getByLabel("Name").fill("Checkout v2 (new)");
  await page.getByLabel("Owner").fill("payments");
  await page.getByLabel("Tags").fill("checkout, money");
  await expect(page.getByLabel("Visible to client keys")).not.toBeChecked();
  await page.getByLabel("Visible to client keys").check();
  await page.getByRole("button", { name: "Save details" }).click();
  await expect(notice(page)).toContainText("Details saved");

  await page.reload();
  await expect(page.getByLabel("Name")).toHaveValue("Checkout v2 (new)");
  await expect(page.getByLabel("Owner")).toHaveValue("payments");
  await expect(page.getByLabel("Tags")).toHaveValue("checkout, money");
  await expect(page.getByLabel("Visible to client keys")).toBeChecked();

  // And back: unticking it is saved too.
  await page.getByLabel("Visible to client keys").uncheck();
  await page.getByRole("button", { name: "Save details" }).click();
  await expect(notice(page)).toContainText("Details saved");
  await page.reload();
  await expect(page.getByLabel("Name")).toBeEnabled();
  await expect(page.getByLabel("Visible to client keys")).not.toBeChecked();
});

test("an org target, an org clause and an org rollout are saved and shown after a reload", async ({
  page,
}) => {
  await openCheckout(page);
  await page.getByRole("button", { name: "Add a target for another kind" }).click();
  const targets = page.getByRole("region", { name: "Individual targets" });
  await targets.getByLabel("Serve", { exact: true }).selectOption("1");
  await targets.getByLabel("Context kind", { exact: true }).fill("org");
  await targets.getByLabel("Context keys").fill("acme\nglobex");
  await rule(page, 1).getByLabel("Context kind", { exact: true }).fill("org");
  await rule(page, 1).getByLabel("Bucket by context kind").fill("org");
  await save(page).click();
  await expect(notice(page)).toContainText("Targeting saved");

  await page.reload();
  await expect(targets.getByLabel("Context kind", { exact: true })).toHaveValue("org");
  await expect(targets.getByLabel("Serve", { exact: true })).toHaveValue("1");
  await expect(targets.getByLabel("Context keys")).toHaveValue("acme\nglobex");
  await expect(rule(page, 1).getByLabel("Context kind", { exact: true })).toHaveValue("org");
  await expect(rule(page, 1).getByLabel("Bucket by context kind")).toHaveValue("org");
  // The user target box is still only the user target.
  await expect(page.getByLabel("Serve 0: true")).toHaveValue("alice\nbob");
});
