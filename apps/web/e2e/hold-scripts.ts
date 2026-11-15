import type { Page, Route } from "@playwright/test";

/** Holds every client script request until `release` runs, so React cannot hydrate before then. */
export async function holdScripts(page: Page) {
  const held: Route[] = [];
  let release!: () => void;
  const promise = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route(/\/assets\/[^?]*\.js(\?.*)?$/, async (route) => {
    held.push(route);
    await promise;
    await route.continue();
  });
  return { held, release };
}
