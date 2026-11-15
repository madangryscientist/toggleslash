import { within } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import { createKey, loadKeys, revokeKey } from "#/api/operator";
import { signOut } from "#/api/session";
import { apiKey, scope } from "#/test/fixtures";
import { render } from "#/test/render";
import { Route } from "../keys";

vi.mock("#/api/operator", () => ({
  loadKeys: vi.fn<typeof loadKeys>(),
  createKey: vi.fn<typeof createKey>(),
  revokeKey: vi.fn<typeof revokeKey>(),
}));
vi.mock("#/api/session", () => ({ signOut: vi.fn<typeof signOut>() }));

const PAGE = { route: Route, path: "/keys" };
const URL = "/keys?project=shop&env=production";
const backend = apiKey(1, "Backend");
const old = apiKey(2, "Old laptop", { revokedAt: "2026-11-03T09:00:00Z" });

beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(loadKeys).mockResolvedValue({ ...scope(), keys: [backend, old] });
});

test("lists each key by its prefix, and only an active one can be revoked", async () => {
  const page = await render(PAGE, { url: URL });
  const active = page.getByRole("row", { name: /^Backend/ });
  expect(active.textContent).toContain("tgl_srv_a1b2c3d1");
  expect(within(active).getByRole("button", { name: "Revoke Backend" })).toBeTruthy();
  const revoked = page.getByRole("row", { name: /^Old laptop/ });
  expect(revoked.textContent).toContain("revoked");
  expect(within(revoked).queryByRole("button", { name: /Revoke/ })).toBeNull();
});

test("an environment with no keys offers to create the first", async () => {
  vi.mocked(loadKeys).mockResolvedValue({ ...scope(), keys: [] });
  const page = await render(PAGE, { url: URL });
  expect(page.getByText("No keys yet")).toBeTruthy();
  await page.user.click(page.getByRole("button", { name: "Create the first key" }));
  expect(page.getByRole("dialog", { name: "New SDK key" })).toBeTruthy();
});

test("a key needs a name before it is asked for", async () => {
  const page = await render(PAGE, { url: URL });
  await page.user.click(page.getByRole("button", { name: "New key" }));
  const dialog = within(page.getByRole("dialog", { name: "New SDK key" }));
  await page.user.click(dialog.getByRole("button", { name: "Create key" }));
  expect(dialog.getByRole("alert").textContent).toBe(
    "Give the key a name, so you can tell it from the others later.",
  );
  expect(createKey).not.toHaveBeenCalled();
});

test("dismissing a new key's secret removes it from the page", async () => {
  vi.mocked(createKey).mockResolvedValue({
    ok: true,
    saved: {
      ...apiKey(3, "Browser", { kind: "client", prefix: "tgl_cli_00000001" }),
      plaintext: "tgl_cli_00000001SECRETPART",
    },
  });
  const page = await render(PAGE, { url: URL });
  await page.user.click(page.getByRole("button", { name: "New key" }));
  const dialog = within(page.getByRole("dialog", { name: "New SDK key" }));
  await page.user.type(dialog.getByRole("textbox", { name: "Name" }), "Browser");
  await page.user.selectOptions(dialog.getByRole("combobox", { name: "Kind" }), "client");
  await page.user.click(dialog.getByRole("button", { name: "Create key" }));
  expect(createKey).toHaveBeenCalledWith({
    data: { project: "shop", env: "production", name: "Browser", kind: "client" },
  });
  expect(await page.findByText("tgl_cli_00000001SECRETPART")).toBeTruthy();

  await page.user.click(page.getByRole("button", { name: "I have copied it" }));
  expect(page.container.textContent).not.toContain("SECRETPART");
});

test("Cancel on the revoke question keeps the key and revokes nothing", async () => {
  const page = await render(PAGE, { url: URL });
  await page.user.click(page.getByRole("button", { name: "Revoke Backend" }));
  const dialog = within(page.getByRole("dialog", { name: "Revoke Backend?" }));
  await page.user.click(dialog.getByRole("button", { name: "Cancel" }));
  expect(page.queryByRole("dialog")).toBeNull();
  expect(page.getByRole("button", { name: "Revoke Backend" })).toBeTruthy();
  expect(revokeKey).not.toHaveBeenCalled();
});
