import { beforeEach, expect, test, vi } from "vitest";
import {
  archiveFlag,
  createFlag,
  deleteFlag,
  loadDashboard,
  restoreFlag,
  toggleFlag,
} from "#/api/dashboard";
import { signOut } from "#/api/session";
import { dashboard, row, shopRows } from "#/test/fixtures";
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
