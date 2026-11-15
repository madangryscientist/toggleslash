import { expect, test as setup } from "@playwright/test";
import { SKIP_ENV, hydrated, signedInState, stackInfo } from "./setup.ts";

setup.skip(process.env[SKIP_ENV] !== undefined, process.env[SKIP_ENV] ?? "");

// Signs in through the real form once, with the password this run's stack was started with, and
// saves the session for the journey. The file is under the run's TMPDIR, removed with it.
setup("sign in to the dashboard through the real form", async ({ page }) => {
  const info = stackInfo();
  await page.goto(info.dashboardUrl);
  await hydrated(page);
  await page.getByLabel("Password").fill(info.operatorPassword);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Flags" })).toBeVisible();
  await page.context().storageState({ path: signedInState() });
});
