import { expect, type Page } from "@playwright/test";

/**
 * Opens `url` and resolves once React has hydrated the page: the root sets `data-hydrated` on
 * `<html>` only then. Before it, a field shows its server-rendered value but is not wired yet, so a
 * click or a fill can be lost. Answers the navigation's response, as `page.goto` does.
 */
export async function gotoHydrated(page: Page, url: string) {
  const response = await page.goto(url);
  await expect(page.locator("html")).toHaveAttribute("data-hydrated", "");
  return response;
}
