import { expect, test, type Page, type Route } from "@playwright/test";
import { API_PORT, E2E_ADMIN_TOKEN } from "./constants.ts";

const API = `http://127.0.0.1:${API_PORT}`;
const CONFIG = `${API}/api/v1/projects/shop/flags/checkout-v2/environments/production`;
const AUTH = { authorization: `Bearer ${E2E_ADMIN_TOKEN}` };

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

/** Holds every client script request until `release` runs, so React cannot hydrate before then. */
async function holdScripts(page: Page) {
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

test("typing is refused until React hydrates, and a fill after it saves exactly what was typed", async ({
  page,
  request,
}) => {
  const { held, release } = await holdScripts(page);

  // The default wait is the load event, which held module scripts block.
  await page.goto("/flags/checkout-v2?project=shop&env=production", { waitUntil: "commit" });
  await expect(page.getByRole("heading", { name: "Checkout v2" })).toBeVisible();
  // The server-rendered field shows its value and cannot be typed into yet.
  await expect(page.getByTestId("targets-0")).toHaveValue("alice\nbob");
  await expect(page.getByTestId("targets-0")).toBeDisabled();
  expect(held.length).toBeGreaterThan(0);

  release();
  await expect(page.getByTestId("targets-0")).toBeEnabled();
  await page.getByTestId("targets-0").fill("alice\nbob\ncarol");
  await page.getByTestId("save-targeting").click();
  await expect(page.getByTestId("notice")).toContainText("Targeting saved");

  const stored = await (await request.get(CONFIG, { headers: AUTH })).json();
  const target = stored.targets.find((t: { variation: number }) => t.variation === 0);
  expect(target.contextKeys).toEqual(["alice", "bob", "carol"]);
});
