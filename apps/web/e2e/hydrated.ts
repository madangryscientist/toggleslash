import type { Page } from "@playwright/test";

/**
 * Resolves once React has taken over the element matched by `selector`. Until then the field shows
 * the server-rendered value but is not yet wired, so text typed into it merges with the value React
 * restores on hydration. Waiting for the value alone does not prove that; this does.
 */
export async function waitForHydrated(page: Page, selector: string) {
  await page.waitForFunction((sel) => {
    const el = document.querySelector(sel);
    return (
      !!el &&
      Object.keys(el).some((k) => k.startsWith("__reactProps$") || k.startsWith("__reactFiber$"))
    );
  }, selector);
}
