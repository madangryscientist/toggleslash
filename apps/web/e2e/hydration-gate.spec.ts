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
      page.getByTestId("new-flag"),
      page.getByLabel("Search flags"),
      page.getByRole("radio", { name: "Archived" }),
      page.locator("[data-testid^='toggle-']").first(),
      page.getByRole("button", { name: /^Archive / }).first(),
    ],
  },
  {
    name: "/environments",
    url: "/environments?project=shop&env=production",
    environmentPicker: false,
    controls: (page) => [
      page.getByTestId("new-environment"),
      page.getByTestId("rename-production"),
    ],
  },
  {
    name: "/segments",
    url: "/segments?project=shop&env=production",
    environmentPicker: true,
    controls: (page) => [page.getByTestId("new-segment"), page.getByTestId("edit-beta-testers")],
  },
  {
    name: "/keys",
    url: "/keys?project=shop&env=production",
    environmentPicker: true,
    controls: (page) => [page.getByTestId("new-key-button"), page.getByTestId("revoke-1")],
  },
  {
    name: "/audit",
    url: "/audit?project=shop",
    environmentPicker: false,
    controls: (page) => [page.getByTestId("apply-filters")],
  },
];

for (const { name, url, environmentPicker, controls } of PAGES) {
  test(`${name}: its controls are disabled until React hydrates, then enabled`, async ({
    page,
  }) => {
    const { held, release } = await holdScripts(page);

    // The default wait is the load event, which held module scripts block.
    await page.goto(url, { waitUntil: "commit" });
    const header = [page.getByLabel("Project"), page.getByTestId("sign-out")];
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
    page.getByTestId("meta-name"),
    page.getByTestId("meta-owner"),
    page.getByLabel("Visible to client keys"),
    page.getByTestId("sign-out"),
    page.getByLabel("Environment"),
  ];
  await expect(page.getByTestId("meta-name")).toBeVisible();
  for (const control of gated) await expect(control).toBeDisabled();
  await expect(page.getByTestId("save-meta")).toBeDisabled();
  expect(held.length).toBeGreaterThan(0);

  release();
  for (const control of gated) await expect(control).toBeEnabled();
  await page.getByTestId("meta-name").fill("Checkout v2 (gated)");
  await expect(page.getByTestId("save-meta")).toBeEnabled();
});
