import { mkdirSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { createCli, type Cli } from "../src/cli.ts";
import { record, text } from "../src/data.ts";
import { SEED_ENV, SEED_PROJECT } from "../src/seed.ts";
import { SKIP_ENV, STACK_ENV, type StackInfo } from "./setup.ts";

/**
 * One person's afternoon with the whole product, on the real stack: the dashboard in a browser,
 * two shoppers on the example app, and the CLI as a subprocess. Every step is something a user
 * sees or a command prints. Each wait is an `expect` that polls; nothing sleeps.
 */
const FLAG = "journey-flag";
const PRO = { key: "pro-user", attributes: { plan: "pro", country: "NL" } };

test.skip(process.env[SKIP_ENV] !== undefined, process.env[SKIP_ENV] ?? "");

const stack = (): StackInfo => {
  const raw = process.env[STACK_ENV];
  if (raw === undefined) throw new Error("the stack was not started (stack/setup.ts)");
  return JSON.parse(raw);
};

const cell = (page: Page, flag: string, field: "value" | "reason") =>
  page.getByTestId(`flag-${flag}-${field}`);

interface Journey {
  info: StackInfo;
  home: string;
  cli: Cli;
  scope: string[];
  dashboard: Page;
  free: Page;
  pro: Page;
}

const FLAGS = ["new-checkout", "banner-text", "discount-percent", "checkout-config", "legacy-cart"];

const served = (flags: unknown, key: string): unknown =>
  Array.isArray(flags) ? flags.map(record).find((f) => f["key"] === key)?.["value"] : undefined;

async function signIn({ info, dashboard }: Journey) {
  await dashboard.goto(info.dashboardUrl);
  await dashboard.getByLabel("Password").fill(info.operatorPassword);
  await dashboard.getByTestId("sign-in-submit").click();
  await expect(dashboard.getByRole("heading", { name: "Flags" })).toBeVisible();
  await dashboard.getByLabel("Project").selectOption(SEED_PROJECT);
  await dashboard.getByLabel("Environment").selectOption(SEED_ENV);
  for (const key of FLAGS) await expect(dashboard.getByTestId(`flag-${key}`)).toBeVisible();
  await expect(dashboard.getByTestId("toggle-new-checkout")).toHaveText("On");
}

async function shoppersDiffer({ free, pro }: Journey) {
  await expect(cell(free, "new-checkout", "value")).toHaveText("false");
  await expect(cell(free, "new-checkout", "reason")).toHaveText("FALLTHROUGH");
  await expect(cell(pro, "new-checkout", "value")).toHaveText("true");
  await expect(cell(pro, "new-checkout", "reason")).toHaveText("RULE_MATCH");
  // The flag that does not exist yet is the app's fallback, and says so.
  await expect(cell(pro, FLAG, "reason")).toHaveText("ERROR");
}

async function createFlagWithRule({ dashboard, free, pro }: Journey) {
  await dashboard.getByTestId("nav-flags").click();
  await dashboard.getByTestId("new-flag").click();
  await dashboard.getByLabel("Name").fill("Journey flag");
  await dashboard.getByLabel("Key").fill(FLAG);
  await dashboard.getByRole("button", { name: "Create flag" }).click();
  await expect(dashboard.getByTestId(`flag-${FLAG}`)).toBeVisible();

  await dashboard.getByTestId(`open-${FLAG}`).click();
  await expect(dashboard.getByRole("heading", { name: "Journey flag" })).toBeVisible();
  await dashboard.getByTestId("targeting-on").check();
  await dashboard.getByTestId("add-rule").click();
  await dashboard.getByTestId("rule-0-clause-0-op").selectOption("in");
  await dashboard.getByTestId("rule-0-clause-0-attribute").fill("plan");
  await dashboard.getByTestId("rule-0-clause-0-values").fill("pro");
  // A new flag serves its first variation (true) to everyone; the fallthrough serves the second.
  await dashboard.getByTestId("fallthrough-variation").selectOption("1");
  await dashboard.getByTestId("save-targeting").click();
  await expect(dashboard.getByTestId("notice")).toContainText("Targeting saved");

  // Both open pages follow with no reload: the new ruleset arrives through the stream.
  await expect(cell(pro, FLAG, "reason")).toHaveText("RULE_MATCH");
  await expect(cell(pro, FLAG, "value")).toHaveText("true");
  await expect(cell(free, FLAG, "value")).toHaveText("false");
  await expect(cell(free, FLAG, "reason")).toHaveText("FALLTHROUGH");
}

async function toggleNewCheckout({ dashboard, free, pro }: Journey) {
  await dashboard.getByTestId("nav-flags").click();
  await dashboard.getByTestId("toggle-new-checkout").click();
  await expect(dashboard.getByTestId("toggle-new-checkout")).toHaveText("Off");
  // The seeded flag's off variation and its fallthrough are both false, so the free shopper's
  // value is false either way; what changes for them is why. The pro shopper's value changes.
  await expect(cell(pro, "new-checkout", "value")).toHaveText("false");
  await expect(cell(pro, "new-checkout", "reason")).toHaveText("OFF");
  await expect(cell(free, "new-checkout", "value")).toHaveText("false");
  await expect(cell(free, "new-checkout", "reason")).toHaveText("OFF");
}

async function streamAndEtag({ cli, scope, free, pro }: Journey) {
  await expect(pro.getByTestId("status-stream")).toHaveText("connected");
  await expect(pro.getByTestId("status-last-error")).toHaveText("-");
  const etag = text(record(await cli.json(["runtime", ...scope]))["etag"]);
  await expect(pro.getByTestId("status-etag")).toHaveText(etag);
  await expect(free.getByTestId("status-etag")).toHaveText(etag);
}

async function diagnoseAgrees({ cli, scope, pro }: Journey) {
  const answer = record(
    await cli.json(["diagnose", "new-checkout", ...scope, "--context", JSON.stringify(PRO)]),
  );
  const diagnosis = record(record(answer["flags"])["new-checkout"]);
  await expect(cell(pro, "new-checkout", "value")).toHaveText(JSON.stringify(diagnosis["value"]));
  await expect(cell(pro, "new-checkout", "reason")).toHaveText(
    text(record(diagnosis["reason"])["kind"]),
  );
}

async function browserEvaluates({ info, pro }: Journey) {
  await pro.getByTestId("evaluate-button").click();
  await expect(pro.getByTestId("evaluate-status")).toHaveText("200");
  const answered = record(JSON.parse(await pro.getByTestId("evaluate-result").innerText()));
  const state = record(
    await (await pro.request.get(`${info.exampleUrl}/state?user=pro-user&flag=${FLAG}`)).json(),
  );
  for (const key of ["new-checkout", FLAG]) {
    expect(record(record(answered["flags"])[key])["value"]).toBe(served(state["flags"], key));
  }
}

async function usageAndAudit({ info, dashboard }: Journey) {
  await dashboard.goto(
    `${info.dashboardUrl}/flags/new-checkout?project=${SEED_PROJECT}&env=${SEED_ENV}`,
  );
  // Usage arrives with the app's flush (two seconds in this stack); reload until it is there.
  await expect(async () => {
    await dashboard.reload();
    const usage = dashboard.getByTestId(`usage-${SEED_ENV}`);
    await expect(usage).toContainText("true:", { timeout: 1_000 });
    await expect(usage).toContainText("false:", { timeout: 1_000 });
  }).toPass({ timeout: 30_000 });
  await expect(dashboard.getByTestId(`usage-total-${SEED_ENV}`)).toContainText(/[1-9]/);

  await dashboard.goto(`${info.dashboardUrl}/audit?project=${SEED_PROJECT}&env=${SEED_ENV}`);
  await expect(dashboard.getByTestId("audit-events")).toContainText(
    `toggled flag_config new-checkout/${SEED_ENV} in ${SEED_ENV}`,
  );
}

async function readTokenCannotWrite({ info, home, cli, scope, dashboard }: Journey) {
  const created = record(
    await cli.json([
      "tokens",
      "create",
      "--project",
      SEED_PROJECT,
      "--name",
      "journey-read",
      "--permission",
      "read",
      "--expires-in-days",
      "1",
    ]),
  );
  const scoped = createCli({ url: info.serverUrl, token: text(created["token"]), home });
  expect((await scoped.run(["runtime", ...scope])).code).toBe(0);
  const refused = await scoped.run(["flags", "on", "new-checkout", ...scope]);
  expect(refused.code).toBe(1);
  expect(refused.stderr).toContain("403");
  // The refusal changed nothing: the dashboard still shows the flag off.
  await dashboard.goto(`${info.dashboardUrl}/?project=${SEED_PROJECT}&env=${SEED_ENV}`);
  await expect(dashboard.getByTestId("toggle-new-checkout")).toHaveText("Off");
}

test("a flag changed in the dashboard reaches the shop, the CLI and the usage counts", async ({
  browser,
}) => {
  const info = stack();
  const root = mkdtempSync(join(tmpdir(), "featureslash-journey-"));
  const home = join(root, "home");
  mkdirSync(home);
  const context = await browser.newContext();
  // The example page reads one flag it does not know with `?flag=`, so there is a flag to watch
  // before the dashboard creates it.
  const shopper = async (user: string) => {
    const page = await context.newPage();
    await page.goto(`${info.exampleUrl}/?user=${user}&flag=${FLAG}`);
    return page;
  };
  const journey: Journey = {
    info,
    home,
    cli: createCli({ url: info.serverUrl, token: info.adminToken, home }),
    scope: ["--project", SEED_PROJECT, "--env", SEED_ENV],
    dashboard: await context.newPage(),
    free: await shopper("free-user"),
    pro: await shopper("pro-user"),
  };

  await test.step("sign in with the real form and see the seeded flags", () => signIn(journey));
  await test.step("the free and the pro shopper see different new-checkout values", () =>
    shoppersDiffer(journey));
  await test.step("create a flag in the dashboard with a rule: plan is pro", () =>
    createFlagWithRule(journey));
  await test.step("toggle new-checkout and both shoppers follow with no reload", () =>
    toggleNewCheckout(journey));
  await test.step("the stream is connected and the ruleset is the one the server reports", () =>
    streamAndEtag(journey));
  await test.step("featureslash diagnose agrees with the page for the pro shopper", () =>
    diagnoseAgrees(journey));
  await test.step("the browser evaluate button answers what /state says", () =>
    browserEvaluates(journey));
  await test.step("the flag's page counts what the app served and the audit log shows the toggle", () =>
    usageAndAudit(journey));
  await test.step("a read-scoped token reads runtime and is refused a toggle", () =>
    readTokenCannotWrite(journey));
});
