import { expect, test, type Page } from "@playwright/test";
import { API_PORT } from "./constants.ts";

const API = `http://127.0.0.1:${API_PORT}`;

// A phone: below `md`, the header keeps the brand and the Menu, and the pickers, Sign out and the
// sections sit in the Menu.
test.use({ viewport: { width: 390, height: 844 } });

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

/** A section link in the dashboard nav. The nav is also in a phone menu, hidden on desktop, so match by role. */
const section = (page: Page, label: string) =>
  page
    .getByRole("navigation", { name: "Sections" })
    .getByRole("link", { name: label, exact: true });

/** The Menu button in the phone header. Its panel opens when it is clicked. */
const openMenu = (page: Page) => page.getByText("Menu", { exact: true }).click();

test("the phone menu switches the environment and stays on the page", async ({ page }) => {
  await page.goto("/keys?project=shop&env=production");
  await openMenu(page);
  await page.getByLabel("Environment").selectOption("staging");
  await expect(page).toHaveURL(/\/keys\?.*env=staging/);
  await expect(page.getByLabel("Environment")).toHaveValue("staging");
});

test("a section link in the phone menu opens its page", async ({ page }) => {
  await page.goto("/?project=shop&env=staging");
  await openMenu(page);
  await section(page, "Segments").click();
  await expect(page).toHaveURL(/\/segments/);
  // The URL changes before the Segments page has loaded; wait for its page before reading the nav.
  await expect(page.getByRole("button", { name: "New segment" })).toBeEnabled();
  // Choosing a link closes the Menu, so open it again to see which section is current.
  await openMenu(page);
  await expect(section(page, "Segments")).toHaveAttribute("aria-current", "page");
});
