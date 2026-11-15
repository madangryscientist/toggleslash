import { within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import {
  archiveFlag,
  createFlag,
  deleteFlag,
  loadDashboard,
  restoreFlag,
  toggleFlag,
} from "#/api/dashboard";
import { signOut } from "#/api/session";
import { dashboard, docs, environment, row, shopRows } from "#/test/fixtures";
import { render } from "#/test/render";
import { Route } from "../index";

vi.mock("#/api/dashboard", () => ({
  loadDashboard: vi.fn<typeof loadDashboard>(),
  toggleFlag: vi.fn<typeof toggleFlag>(),
  createFlag: vi.fn<typeof createFlag>(),
  archiveFlag: vi.fn<typeof archiveFlag>(),
  restoreFlag: vi.fn<typeof restoreFlag>(),
  deleteFlag: vi.fn<typeof deleteFlag>(),
}));
vi.mock("#/api/session", () => ({ signOut: vi.fn<typeof signOut>() }));

const load = vi.mocked(loadDashboard);
const PAGE = { route: Route, path: "/" };

beforeEach(() => {
  vi.resetAllMocks();
});

test("a server that is down is a page with a way to try again, and it comes back", async () => {
  load.mockResolvedValueOnce({
    ok: false,
    failure: { kind: "unreachable", message: "connect ECONNREFUSED 127.0.0.1:4421" },
  });
  const page = await render(PAGE);
  const down = { name: "The ToggleSlash server is not reachable" };
  expect(page.getByRole("heading", down)).toBeTruthy();
  expect(page.queryByRole("table")).toBeNull();

  load.mockResolvedValue(dashboard());
  await page.user.click(page.getByRole("button", { name: /try again/i }));
  expect(await page.findByRole("button", { name: "Checkout v2 in Production" })).toBeTruthy();
  expect(page.queryByRole("heading", down)).toBeNull();
});

test("a stale toggle says so and shows what the server has", async () => {
  load.mockResolvedValue(dashboard());
  const page = await render(PAGE);
  const toggle = page.getByRole("button", { name: "Dark mode in Production" });
  expect(toggle.textContent).toBe("Off");

  // Someone else turned it on after this page loaded: the toggle is refused, and the list reloads.
  vi.mocked(toggleFlag).mockResolvedValue({
    ok: false,
    failure: { kind: "error", message: "The flag changed since you loaded it." },
    conflict: true,
  });
  const [checkout = row("checkout-v2", "Checkout v2")] = shopRows;
  load.mockResolvedValue(
    dashboard([checkout, row("dark-mode", "Dark mode", { on: true, version: 2 })]),
  );
  await page.user.click(toggle);
  expect(await page.findByText("The flag changed since you loaded it.")).toBeTruthy();
  expect(page.getByRole("button", { name: "Dark mode in Production" }).textContent).toBe("On");
  expect(toggleFlag).toHaveBeenCalledWith({
    data: { project: "shop", env: "production", flag: "dark-mode", on: true, version: 1 },
  });
});

test("a search that matches nothing says so, and clearing it brings the list back", async () => {
  load.mockResolvedValue(dashboard());
  const page = await render(PAGE);
  const search = page.getByRole("searchbox", { name: "Search flags" });
  await page.user.type(search, "nothing-like-this");
  expect(page.getByText("No flags match")).toBeTruthy();
  expect(page.queryByRole("table")).toBeNull();
  await page.user.clear(search);
  expect(page.getByRole("button", { name: "Dark mode in Production" })).toBeTruthy();
});

test("a project with no flags offers to create the first", async () => {
  load.mockResolvedValue(dashboard([]));
  const page = await render(PAGE);
  expect(page.getByText("No flags yet")).toBeTruthy();
  await page.user.click(page.getByRole("button", { name: "Create the first flag" }));
  expect(page.getByRole("dialog", { name: "New flag" })).toBeTruthy();
});

/** Opens New flag, types a name (when given) and a key, and submits. */
async function newFlag(name: string, key: string) {
  load.mockResolvedValue(dashboard());
  const page = await render(PAGE);
  await page.user.click(page.getByRole("button", { name: "New flag" }));
  const dialog = within(page.getByRole("dialog", { name: "New flag" }));
  if (name !== "") await page.user.type(dialog.getByRole("textbox", { name: "Name" }), name);
  await page.user.type(dialog.getByRole("textbox", { name: /^Key/ }), key);
  await page.user.click(dialog.getByRole("button", { name: "Create flag" }));
  return { page, dialog };
}

describe("creating a flag", () => {
  test("a bad key is caught before it is sent", async () => {
    const { dialog } = await newFlag("Bad", "Not A Key");
    // The key's field marks it as it is typed, and the submit says it again under the form.
    const alerts = dialog.getAllByRole("alert").map((alert) => alert.textContent);
    expect(alerts).toContain("Give the flag a key: lowercase letters, digits, - and _, up to 64.");
    expect(createFlag).not.toHaveBeenCalled();
  });

  test("a blank name is caught before it is sent", async () => {
    const { dialog } = await newFlag("", "fine-key");
    expect(dialog.getByRole("alert").textContent).toBe("Give the flag a name.");
    expect(createFlag).not.toHaveBeenCalled();
  });

  test("a taken key shows the server's reason and keeps the dialog", async () => {
    vi.mocked(createFlag).mockResolvedValue({
      ok: false,
      failure: { kind: "error", message: "A flag with key dark-mode already exists." },
      conflict: true,
    });
    const { page, dialog } = await newFlag("Again", "dark-mode");
    expect((await dialog.findByRole("alert")).textContent).toBe(
      "A flag with key dark-mode already exists.",
    );
    expect(page.getByRole("dialog", { name: "New flag" })).toBeTruthy();
  });
});

test("Cancel on the archive question keeps the flag and archives nothing", async () => {
  load.mockResolvedValue(dashboard());
  const page = await render(PAGE);
  await page.user.click(page.getByRole("button", { name: "Archive Dark mode" }));
  const dialog = within(page.getByRole("dialog", { name: "Archive Dark mode?" }));
  await page.user.click(dialog.getByRole("button", { name: "Cancel" }));
  expect(page.queryByRole("dialog")).toBeNull();
  expect(page.getByRole("button", { name: "Dark mode in Production" })).toBeTruthy();
  expect(archiveFlag).not.toHaveBeenCalled();
});

describe("the archived view", () => {
  const archived = [
    row("old-banner", "Old banner", { lifecycle: "archived", on: null, version: null }),
    row("legacy-cart", "Legacy cart", { lifecycle: "archived", on: null, version: null }),
  ];

  test("says so when the project has none", async () => {
    load.mockResolvedValue({
      ...dashboard([], true),
      project: docs,
      environments: [environment("live", "Live")],
      environment: environment("live", "Live"),
    });
    const page = await render(PAGE, { url: "/?project=docs&archived=true" });
    expect(page.getByText("No archived flags in Docs.")).toBeTruthy();
    expect(load).toHaveBeenCalledWith({
      data: { project: "docs", env: undefined, archived: true },
    });
  });

  test("Cancel on the delete question keeps the flag and deletes nothing", async () => {
    load.mockResolvedValue(dashboard(archived, true));
    const page = await render(PAGE, { url: "/?archived=true" });
    await page.user.click(page.getByRole("button", { name: "Delete Old banner" }));
    const dialog = within(page.getByRole("dialog", { name: "Delete Old banner?" }));
    await page.user.click(dialog.getByRole("button", { name: "Cancel" }));
    expect(page.queryByRole("dialog")).toBeNull();
    expect(page.getByRole("button", { name: "Restore Old banner" })).toBeTruthy();
    expect(deleteFlag).not.toHaveBeenCalled();
  });

  test("a delete the server refuses shows its message and keeps the flag", async () => {
    load.mockResolvedValue(dashboard(archived, true));
    vi.mocked(deleteFlag).mockResolvedValue({
      ok: false,
      failure: {
        kind: "error",
        message: 'Flag "legacy-cart" is a prerequisite of "search-boost". Remove it there first.',
      },
      conflict: true,
    });
    const page = await render(PAGE, { url: "/?archived=true" });
    await page.user.click(page.getByRole("button", { name: "Delete Legacy cart" }));
    const dialog = within(page.getByRole("dialog", { name: "Delete Legacy cart?" }));
    await page.user.click(dialog.getByRole("button", { name: "Delete" }));
    expect(await page.findByText(/is a prerequisite of "search-boost"/)).toBeTruthy();
    expect(page.getByRole("button", { name: "Restore Legacy cart" })).toBeTruthy();
  });
});
