import { expect, test, type Locator, type Page } from "@playwright/test";
import { API_PORT } from "./constants.ts";
import { holdScripts } from "./hold-scripts.ts";

const API = `http://127.0.0.1:${API_PORT}`;

test.beforeEach(async ({ request }) => {
  await request.post(`${API}/__reset`);
});

/**
 * Every control a page shows on its first paint, before React has attached its handlers. Each must
 * be disabled until hydration, so a click or a keystroke cannot land on a control that does nothing.
 */
/** Each page's shell header is checked too. Pages that hide the environment picker (environments, audit) are marked. */
const PAGES: {
  name: string;
  url: string;
  environmentPicker: boolean;
  controls: (page: Page) => Locator[];
}[] = [
  {
    name: "/",
    url: "/",
    environmentPicker: true,
    controls: (page) => [
      page.getByRole("button", { name: "New flag" }),
      page.getByLabel("Search flags"),
      page.getByRole("radio", { name: "Archived" }),
      page.getByRole("button", { name: "Checkout v2 in Production" }),
      page.getByRole("button", { name: "Archive Checkout v2" }),
    ],
  },
  {
    name: "/?archived=true",
    url: "/?archived=true",
    environmentPicker: true,
    controls: (page) => [
      page.getByRole("button", { name: "Restore Old banner" }),
      page.getByRole("button", { name: "Delete Old banner" }),
    ],
  },
  {
    name: "/environments",
    url: "/environments?project=shop&env=production",
    environmentPicker: false,
    controls: (page) => [
      page.getByRole("button", { name: "New environment" }),
      page.getByRole("button", { name: "Rename production" }),
      page.getByRole("button", { name: "Signing key for production" }),
    ],
  },
  {
    name: "/segments",
    url: "/segments?project=shop&env=production",
    environmentPicker: true,
    controls: (page) => [
      page.getByRole("button", { name: "New segment" }),
      page.getByRole("button", { name: "Edit beta-testers" }),
    ],
  },
  {
    name: "/keys",
    url: "/keys?project=shop&env=production",
    environmentPicker: true,
    controls: (page) => [
      page.getByRole("button", { name: "New key" }),
      page.getByRole("button", { name: "Revoke Backend" }),
    ],
  },
  {
    name: "/audit",
    url: "/audit?project=shop",
    environmentPicker: false,
    controls: (page) => [page.getByRole("button", { name: "Apply filters" })],
  },
];

for (const { name, url, environmentPicker, controls } of PAGES) {
  test(`${name}: its controls are disabled until React hydrates, then enabled`, async ({
    page,
  }) => {
    const { held, release } = await holdScripts(page);

    // The default wait is the load event, which held module scripts block.
    await page.goto(url, { waitUntil: "commit" });
    const header = [page.getByLabel("Project"), page.getByRole("button", { name: "Sign out" })];
    if (environmentPicker) header.push(page.getByLabel("Environment"));
    const shown = [...header, ...controls(page)];
    for (const control of shown) {
      await expect(control).toBeVisible();
      await expect(control).toBeDisabled();
    }
    expect(held.length).toBeGreaterThan(0);

    release();
    for (const control of shown) {
      await expect(control).toBeEnabled();
    }
  });
}

test("the flag page's name field and save button are disabled until React hydrates", async ({
  page,
}) => {
  const { held, release } = await holdScripts(page);

  // The default wait is the load event, which held module scripts block.
  await page.goto("/flags/checkout-v2?project=shop&env=production", { waitUntil: "commit" });
  const gated = [
    page.getByLabel("Name"),
    page.getByLabel("Owner"),
    page.getByLabel("Visible to client keys"),
    page.getByRole("button", { name: "Sign out" }),
    page.getByLabel("Environment"),
  ];
  const saveDetails = page.getByRole("button", { name: "Save details" });
  await expect(page.getByLabel("Name")).toBeVisible();
  for (const control of gated) await expect(control).toBeDisabled();
  await expect(saveDetails).toBeDisabled();
  expect(held.length).toBeGreaterThan(0);

  release();
  for (const control of gated) await expect(control).toBeEnabled();
  await page.getByLabel("Name").fill("Checkout v2 (gated)");
  await expect(saveDetails).toBeEnabled();
});
