import { expect, test as setup } from "@playwright/test";
import { E2E_OPERATOR_PASSWORD, SIGNED_IN_STATE } from "./constants.ts";

setup("sign in through the real form", async ({ page }) => {
  await page.goto("/");
  await page.getByLabel("Password").fill(E2E_OPERATOR_PASSWORD);
  await page.getByTestId("sign-in-submit").click();
  await expect(page.getByRole("heading", { name: "Flags" })).toBeVisible();
  await page.context().storageState({ path: SIGNED_IN_STATE });
});
