import { within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import {
  createEnvironment,
  createSigningKey,
  deleteEnvironment,
  renameEnvironment,
  setSecureMode,
} from "#/api/environments";
import { loadEnvironments } from "#/api/operator";
import { signOut } from "#/api/session";
import type { SaveResult, SigningKey } from "#/api/types";
import { scope } from "#/test/fixtures";
import { isDisabled, render } from "#/test/render";
import { Route } from "../environments";

vi.mock("#/api/environments", () => ({
  createEnvironment: vi.fn<typeof createEnvironment>(),
  renameEnvironment: vi.fn<typeof renameEnvironment>(),
  deleteEnvironment: vi.fn<typeof deleteEnvironment>(),
  setSecureMode: vi.fn<typeof setSecureMode>(),
  createSigningKey: vi.fn<typeof createSigningKey>(),
}));
vi.mock("#/api/operator", () => ({ loadEnvironments: vi.fn<typeof loadEnvironments>() }));
vi.mock("#/api/session", () => ({ signOut: vi.fn<typeof signOut>() }));

const PAGE = { route: Route, path: "/environments" };
const URL = "/environments?project=shop";

beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(loadEnvironments).mockResolvedValue(scope());
});

async function environments() {
  const page = await render(PAGE, { url: URL });
  return { page, user: page.user };
}

describe("creating an environment", () => {
  test("a bad key is refused before asking, and a duplicate shows the server's refusal", async () => {
    const { page, user } = await environments();
    await user.click(page.getByRole("button", { name: "New environment" }));
    const dialog = within(page.getByRole("dialog", { name: "New environment" }));
    await user.type(dialog.getByRole("textbox", { name: "Name" }), "Prod again");
    const key = dialog.getByRole("textbox", { name: /^Key/ });
    await user.type(key, "Bad Key");
    await user.click(dialog.getByRole("button", { name: "Create environment" }));
    const alerts = dialog.getAllByRole("alert").map((alert) => alert.textContent);
    expect(alerts).toContain(
      "Give the environment a key: lowercase letters, digits, - and _, up to 64.",
    );
    expect(createEnvironment).not.toHaveBeenCalled();

    vi.mocked(createEnvironment).mockResolvedValue({
      ok: false,
      failure: { kind: "error", message: "The server refused it." },
      conflict: true,
      detail: "An environment with key production already exists.",
    });
    await user.clear(key);
    await user.type(key, "production");
    await user.click(dialog.getByRole("button", { name: "Create environment" }));
    expect(
      await dialog.findByText("An environment with key production already exists."),
    ).toBeTruthy();
  });
});

test("a blank name is refused before any request, and Cancel keeps the old one", async () => {
  const { page, user } = await environments();
  await user.click(page.getByRole("button", { name: "Rename staging" }));
  const dialog = within(page.getByRole("dialog", { name: "Rename Staging" }));
  const name = dialog.getByRole("textbox", { name: "Name" });
  await user.clear(name);
  await user.type(name, "   ");
  await user.click(dialog.getByRole("button", { name: "Rename" }));
  expect(dialog.getByRole("alert").textContent).toBe("Give the environment a name.");
  expect(renameEnvironment).not.toHaveBeenCalled();

  await user.click(dialog.getByRole("button", { name: "Cancel" }));
  expect(page.queryByRole("dialog")).toBeNull();
  expect(page.getByRole("row", { name: /^Staging staging/ })).toBeTruthy();
});

describe("deleting an environment", () => {
  test("Cancel keeps the environment and deletes nothing", async () => {
    const { page, user } = await environments();
    await user.click(page.getByRole("button", { name: "Delete staging" }));
    const dialog = within(page.getByRole("dialog", { name: "Delete staging?" }));
    await user.click(dialog.getByRole("button", { name: "Cancel" }));
    expect(page.queryByRole("dialog")).toBeNull();
    expect(page.getByRole("row", { name: /^Staging staging/ })).toBeTruthy();
    expect(deleteEnvironment).not.toHaveBeenCalled();
  });

  test("one with a live key is refused, and the server says so", async () => {
    vi.mocked(deleteEnvironment).mockResolvedValue({
      ok: false,
      failure: {
        kind: "error",
        message:
          'Environment "production" still has 1 SDK key that is not revoked. Revoke them first.',
      },
      conflict: true,
    });
    const { page, user } = await environments();
    await user.click(page.getByRole("button", { name: "Delete production" }));
    const dialog = within(page.getByRole("dialog", { name: "Delete production?" }));
    await user.click(dialog.getByRole("button", { name: "Delete" }));
    expect(await page.findByText(/still has 1 SDK key that is not revoked/)).toBeTruthy();
    expect(page.getByRole("row", { name: /^Production production/ })).toBeTruthy();
  });
});

describe("the signing key and secure mode", () => {
  test("Cancel on the confirm makes no key", async () => {
    const { page, user } = await environments();
    await user.click(page.getByRole("button", { name: "Signing key for staging" }));
    const dialog = page.getByRole("dialog", { name: "Make a signing key for Staging?" });
    expect(dialog.textContent).toContain("every signature made with the old key stops working");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(page.queryByRole("dialog")).toBeNull();
    expect(createSigningKey).not.toHaveBeenCalled();
    expect(page.getByRole("row", { name: /^Staging staging/ }).textContent).toContain(
      "No signing key",
    );
  });

  test("the switch is off and disabled, and says why, until there is a key", async () => {
    const { page } = await environments();
    const toggle = page.getByRole("switch", { name: "Secure mode for staging" });
    expect(isDisabled(toggle)).toBe(true);
    expect(toggle.getAttribute("aria-checked")).toBe("false");
    expect(page.getByRole("row", { name: /^Staging staging/ }).textContent).toContain(
      "Make a signing key first.",
    );
  });

  test("Cancel is off while the key is being made, and the answer lands in the dialog", async () => {
    let answer!: (result: SaveResult<SigningKey>) => void;
    vi.mocked(createSigningKey).mockReturnValue(
      new Promise((resolve) => {
        answer = resolve;
      }),
    );
    const { page, user } = await environments();
    await user.click(page.getByRole("button", { name: "Signing key for staging" }));
    const dialog = page.getByRole("dialog", { name: "Make a signing key for Staging?" });
    await user.click(within(dialog).getByRole("button", { name: "Make key" }));
    // Escape on the native <dialog> needs a real browser; e2e/environments.spec.ts presses it.
    expect(isDisabled(within(dialog).getByRole("button", { name: "Cancel" }))).toBe(true);

    answer({
      ok: true,
      saved: {
        privateKey:
          "-----BEGIN PRIVATE KEY-----\nMC4CAQAwBQYDK2VwBCIEIA\n-----END PRIVATE KEY-----",
        createdAt: "2026-11-15T09:00:00Z",
      },
    });
    const shown = await page.findByRole("dialog", { name: "Signing key for Staging" });
    expect(shown.textContent).toContain("BEGIN PRIVATE KEY");
  });
});
