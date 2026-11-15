import { expect, test, type Page } from "@playwright/test";
import { SEED_ENV, SEED_PROJECT } from "../src/seed.ts";
import { SKIP_ENV, hydrated, signedInState, stackInfo } from "./setup.ts";

/**
 * The whole product on the real stack, in one path: a rule made in the dashboard reaches two
 * shoppers on the example app through the stream, in its server SDK's table and in its browser
 * SDK's. The seed turned secure mode on in dev, so the browser table can only follow if the example
 * signed each shopper's context. Every wait is an `expect` on what a user sees; nothing sleeps.
 */
const FLAG = "journey-flag";

test.skip(process.env[SKIP_ENV] !== undefined, process.env[SKIP_ENV] ?? "");

/** The example's value and reason cells: a `<code>` in a table, with no name of its own. */
const cell = (page: Page, flag: string, field: "value" | "reason") =>
  page.getByTestId(`flag-${flag}-${field}`);
const browserCell = (page: Page, flag: string, field: "value" | "reason") =>
  page.getByTestId(`browser-flag-${flag}-${field}`);

test("a rule made in the dashboard reaches the shop through the stream", async ({ browser }) => {
  const info = stackInfo();
  const context = await browser.newContext({ storageState: signedInState() });
  // The example page reads one flag it does not know with `?flag=`, so there is a flag to watch
  // before the dashboard creates it.
  const shopper = async (user: string) => {
    const page = await context.newPage();
    await page.goto(`${info.exampleUrl}/?user=${user}&flag=${FLAG}`);
    // Not there yet: the app's fallback, and the browser SDK has nothing for it.
    await expect(cell(page, FLAG, "reason")).toHaveText("ERROR");
    return page;
  };
  const free = await shopper("free-user");
  const pro = await shopper("pro-user");

  const dashboard = await context.newPage();
  await dashboard.goto(`${info.dashboardUrl}/?project=${SEED_PROJECT}&env=${SEED_ENV}`);
  await hydrated(dashboard);
  await dashboard.getByRole("button", { name: "New flag" }).click();
  const dialog = dashboard.getByRole("dialog", { name: "New flag" });
  await dialog.getByLabel("Name").fill("Journey flag");
  await dialog.getByLabel("Key", { exact: true }).fill(FLAG);
  // The browser SDK's table must see it, and a client key is answered only flags marked visible.
  await dialog.getByLabel("Visible to client keys").check();
  await dialog.getByRole("button", { name: "Create flag" }).click();

  await dashboard.getByRole("link", { name: "Journey flag" }).click();
  // A client-side navigation: the page was hydrated above, so its controls are live once it shows.
  await expect(dashboard.getByRole("heading", { name: "Journey flag", level: 1 })).toBeVisible();
  await dashboard.getByRole("checkbox", { name: /Targeting is on/ }).check();
  await dashboard.getByRole("button", { name: "Add rule" }).click();
  const rule = dashboard.getByRole("region", { name: "Rule 1" });
  await rule.getByLabel("Operator").selectOption("in");
  await rule.getByLabel("Attribute").fill("plan");
  await rule.getByLabel("Values").fill("pro");
  // A new flag serves its first variation (true) to everyone; the fallthrough serves the second.
  await dashboard
    .getByRole("region", { name: "Fallthrough" })
    .getByLabel("Variation")
    .selectOption("1");
  await dashboard.getByRole("button", { name: "Save targeting" }).click();
  await expect(dashboard.getByRole("status")).toContainText("Targeting saved");

  // Both open pages follow with no reload: the new ruleset arrives through the stream.
  await expect(cell(pro, FLAG, "reason")).toHaveText("RULE_MATCH");
  await expect(cell(pro, FLAG, "value")).toHaveText("true");
  await expect(cell(free, FLAG, "reason")).toHaveText("FALLTHROUGH");
  await expect(cell(free, FLAG, "value")).toHaveText("false");
  // The browser SDK follows too: the change signal tells it to evaluate again, and the server
  // answers its client key only for a context the example signed.
  await expect(browserCell(pro, FLAG, "reason")).toHaveText("RULE_MATCH");
  await expect(browserCell(pro, FLAG, "value")).toHaveText("true");
  await expect(browserCell(free, FLAG, "reason")).toHaveText("FALLTHROUGH");
  await expect(browserCell(free, FLAG, "value")).toHaveText("false");
});
