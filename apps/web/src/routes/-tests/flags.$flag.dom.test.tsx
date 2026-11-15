import { within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import { loadFlagDetail, saveConfig, saveFlagMeta } from "#/api/dashboard";
import { signOut } from "#/api/session";
import { checkout, checkoutConfig, checkoutDetail } from "#/test/fixtures";
import { render, valueOf } from "#/test/render";
import { Route } from "../flags.$flag";

vi.mock("#/api/dashboard", () => ({
  loadFlagDetail: vi.fn<typeof loadFlagDetail>(),
  saveConfig: vi.fn<typeof saveConfig>(),
  saveFlagMeta: vi.fn<typeof saveFlagMeta>(),
}));
vi.mock("#/api/session", () => ({ signOut: vi.fn<typeof signOut>() }));

const load = vi.mocked(loadFlagDetail);
const saveTargeting = vi.mocked(saveConfig);
const saveMeta = vi.mocked(saveFlagMeta);
const PAGE = { route: Route, path: "/flags/$flag" };
const URL = "/flags/checkout-v2?project=shop&env=production";

beforeEach(() => {
  vi.resetAllMocks();
});

test("after a version conflict, a details save reloads the page but keeps the editor on what it read", async () => {
  load.mockResolvedValue(checkoutDetail());
  const page = await render(PAGE, { url: URL });
  const { user } = page;
  const rule = within(page.getByRole("region", { name: "Rule 1" }));
  const values = rule.getByRole("textbox", { name: "Values" });
  await user.clear(values);
  await user.type(values, "NL, DE, BE");
  saveTargeting.mockResolvedValue({
    ok: false,
    failure: { kind: "error", message: "The flag changed since you loaded it." },
    conflict: true,
  });
  await user.click(page.getByRole("button", { name: "Save targeting" }));
  expect(await page.findByText("Someone else changed this flag")).toBeTruthy();

  // Someone else's save made version 4, and the details save reloads the page's data.
  load.mockResolvedValue(checkoutDetail({ ...checkoutConfig, version: 4 }));
  saveMeta.mockResolvedValue({ ok: true, saved: { ...checkout, name: "Checkout renamed" } });
  const name = page.getByRole("textbox", { name: "Name" });
  await user.clear(name);
  await user.type(name, "Checkout renamed");
  await user.click(page.getByRole("button", { name: "Save details" }));
  expect(await page.findByText("The flag's details were saved.")).toBeTruthy();
  expect(load).toHaveBeenCalledTimes(2);

  // The editor was not replaced: the typing, its version and the warning are all still there.
  expect(valueOf(values)).toBe("NL, DE, BE");
  expect(page.getByText("Version 3")).toBeTruthy();
  expect(page.getByText("Someone else changed this flag")).toBeTruthy();
  await user.click(page.getByRole("button", { name: "Save targeting" }));
  expect(saveTargeting).toHaveBeenLastCalledWith({
    data: expect.objectContaining({ version: 3 }),
  });
});

test("an unknown flag shows the server's answer as a page, not a crash", async () => {
  load.mockResolvedValue({
    ok: false,
    failure: { kind: "error", message: 'Flag "nope" was not found in project "shop".' },
  });
  const page = await render(PAGE, { url: "/flags/nope?project=shop&env=production" });
  expect(page.getByRole("heading", { name: "The server could not answer" })).toBeTruthy();
  // The detail is in a code block, whose text the kit splits up.
  expect(page.container.textContent).toContain('Flag "nope" was not found in project "shop".');
  expect(page.queryByRole("button", { name: "Save targeting" })).toBeNull();
});

test("a details save the server refuses marks the field", async () => {
  load.mockResolvedValue(checkoutDetail());
  saveMeta.mockResolvedValue({
    ok: false,
    failure: { kind: "error", message: "The server refused it." },
    detail: "name must not be empty.",
  });
  const page = await render(PAGE, { url: URL });
  await page.user.clear(page.getByRole("textbox", { name: "Name" }));
  await page.user.click(page.getByRole("button", { name: "Save details" }));
  const details = within(page.getByRole("region", { name: "Details" }));
  expect(await details.findByText("name must not be empty.")).toBeTruthy();
  expect(page.queryByText("The flag's details were saved.")).toBeNull();
});

test("shows the flag's history and its usage in the chosen environment", async () => {
  load.mockResolvedValue({
    ...checkoutDetail(),
    history: [
      {
        id: 3,
        at: "2026-11-04T10:00:00Z",
        actor: "admin",
        entityType: "flag_config",
        action: "config.updated",
        environment: "production",
        version: 3,
      },
      {
        id: 1,
        at: "2026-11-01T09:00:00Z",
        actor: "admin",
        entityType: "flag",
        action: "flag.created",
        environment: null,
        version: null,
      },
    ],
    usage: {
      environments: [
        {
          environment: "production",
          total: 42,
          days: [
            {
              day: "2026-11-04",
              count: 30,
              variations: [
                { variation: 0, count: 10 },
                { variation: 1, count: 20 },
              ],
            },
          ],
        },
        { environment: "staging", total: 7, days: [] },
      ],
    },
  });
  const page = await render(PAGE, { url: URL });
  const history = within(page.getByRole("list", { name: "History of this flag" }));
  expect(history.getAllByRole("listitem")).toHaveLength(2);
  expect(history.getByText("config.updated flag_config in production (version 3)")).toBeTruthy();
  expect(history.getByText("2026-11-04 10:00:00 UTC, admin")).toBeTruthy();

  const usage = within(page.getByRole("region", { name: "Usage" }));
  expect(usage.getByText("42")).toBeTruthy();
  const day = usage.getByRole("row", { name: /2026-11-04/ });
  expect(day.textContent).toContain("true: 10, false: 20");
  // Only the chosen environment's counts.
  expect(usage.queryByText("Evaluations in staging")).toBeNull();
});
