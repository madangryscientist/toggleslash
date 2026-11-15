import { expect, type Page } from "@playwright/test";

/**
 * Opens `url` and resolves once React has hydrated the page: the root sets `data-hydrated` on
 * `<html>` only then. Before it, a field shows its server-rendered value but is not wired yet, so a
 * click or a fill can be lost. Answers the navigation's response, as `page.goto` does.
 */
export async function gotoHydrated(page: Page, url: string) {
  const response = await page.goto(url);
  await hydrated(page);
  return response;
}

/** Resolves once `<html>` carries `data-hydrated`. The root element has no role or name, so this reads the attribute in the page. */
export async function hydrated(page: Page) {
  await expect
    .poll(() => page.evaluate(() => document.documentElement.hasAttribute("data-hydrated")))
    .toBe(true);
}
