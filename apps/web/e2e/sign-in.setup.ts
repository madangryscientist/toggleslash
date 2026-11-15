import { expect, test as setup } from "@playwright/test";
import { E2E_OPERATOR_PASSWORD, SIGNED_IN_STATE } from "./constants.ts";
import { gotoHydrated } from "./goto.ts";

setup("sign in through the real form", async ({ page }) => {
  await gotoHydrated(page, "/");
  await page.getByLabel("Password").fill(E2E_OPERATOR_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Flags" })).toBeVisible();
  await page.context().storageState({ path: SIGNED_IN_STATE });
});
