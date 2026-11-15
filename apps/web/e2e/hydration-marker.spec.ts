import { expect, test } from "@playwright/test";
import { gotoHydrated } from "./goto.ts";

// `gotoHydrated` waits on this marker, so it must be absent from what the server sends and set only
// once React has taken over the page. If the server rendered it, every wait would pass too early.
for (const url of ["/", "/sign-in"]) {
  test(`the server render of ${url} carries no hydration marker`, async ({ page }) => {
    const served = await page.request.get(url);
    expect(served.status()).toBe(200);
    const html = await served.text();
    expect(html).toMatch(/<html[^>]*lang="en"/);
    expect(html).not.toContain("data-hydrated");
  });
}

test("a loaded page's root carries the hydration marker", async ({ page }) => {
  await gotoHydrated(page, "/");
  await expect(page.getByRole("heading", { name: "Flags" })).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-hydrated", "");
});
