import { expect, test, type APIRequestContext } from "@playwright/test";

// `gotoHydrated` waits on this marker, so it must be absent from what the server sends and set only
// once React has taken over the page. If the server rendered it, every wait would pass too early.
async function expectNoMarkerServed(request: APIRequestContext, url: string) {
  const served = await request.get(url);
  expect(served.status()).toBe(200);
  // A redirect would leave this reading another page's HTML.
  expect(new URL(served.url()).pathname).toBe(url);
  const html = await served.text();
  expect(html).toContain("<html");
  expect(html).not.toContain("data-hydrated");
}

test("the server render of / carries no hydration marker", async ({ page }) => {
  await expectNoMarkerServed(page.request, "/");
});

test.describe("signed out", () => {
  // Signed in, /sign-in sends the browser on to the dashboard.
  test.use({ storageState: { cookies: [], origins: [] } });

  test("the server render of /sign-in carries no hydration marker", async ({ page }) => {
    await expectNoMarkerServed(page.request, "/sign-in");
  });
});

// A plain goto, not the helper this marker serves, so the check does not lean on itself.
test("a loaded page's root carries the hydration marker", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("html")).toHaveAttribute("data-hydrated", "");
});
