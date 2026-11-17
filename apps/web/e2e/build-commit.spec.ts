import { expect, test } from "@playwright/test";
import { buildCommit } from "../src/build-commit.ts";

/*
 * The production build names the commit it was built from, once, in the head, and shows it nowhere.
 * The expected value is worked out here from the same checkout the build ran in, so `-dirty` is checked too.
 * Both pages are reachable signed out, so the session the other specs start from is not needed.
 */

test.use({ storageState: { cookies: [], origins: [] } });

const expected = buildCommit();
const sha = expected.replace(/-dirty$/, "");

for (const path of ["/", "/sign-in"]) {
  test(`${path} carries the build commit in its head and nowhere visible`, async ({ page }) => {
    await page.goto(path);

    const metas = page.locator("head meta[name='build-commit']");
    await expect(metas).toHaveCount(1);
    await expect(metas).toHaveAttribute("content", expected);

    const visible = await page.locator("body").innerText();
    expect(visible).not.toContain(sha);
  });
}
