import { within } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import { signOut } from "#/api/session";
import type { FailureKind } from "#/api/types";
import { render } from "#/test/render";
import { ServerDown, Shell } from "./Shell";

vi.mock("#/api/session", () => ({ signOut: vi.fn<typeof signOut>() }));

test.each<[FailureKind, string, RegExp]>([
  ["unreachable", "The ToggleSlash server is not reachable", /TOGGLESLASH_API_URL/],
  ["unconfigured", "The dashboard is not configured", /address and the admin token/],
  [
    "unauthorized",
    "The admin API refused the dashboard",
    /TOGGLESLASH_ADMIN_TOKEN.*TOGGLESLASH_ACCESS_CLIENT_ID and TOGGLESLASH_ACCESS_CLIENT_SECRET/,
  ],
  ["error", "The server could not answer", /look at the server's log/],
])("the %s page names what is wrong and what to check", async (kind, title, help) => {
  const onRetry = vi.fn<() => void>();
  const page = await render(
    <ServerDown failure={{ kind, message: "the detail" }} onRetry={onRetry} />,
  );
  expect(page.getByRole("heading", { name: title })).toBeTruthy();
  expect(page.getByText(help)).toBeTruthy();
  // The detail is in a code block, whose text the kit splits up.
  expect(page.container.textContent).toContain("the detail");
  await page.user.click(page.getByRole("button", { name: /try again/i }));
  expect(onRetry).toHaveBeenCalledOnce();
});

test("every section link carries the project and environment along", async () => {
  const page = await render(
    <Shell active="audit" project="shop" env="staging">
      <p>page</p>
    </Shell>,
  );
  const nav = within(page.getAllByRole("navigation", { name: "Sections" })[0] ?? document.body);
  for (const [label, path] of [
    ["Flags", "/"],
    ["Environments", "/environments"],
    ["SDK keys", "/keys"],
    ["Segments", "/segments"],
    ["Audit log", "/audit"],
  ] as const) {
    const href = nav.getByRole("link", { name: label }).getAttribute("href");
    expect(href).toBe(`${path}?project=shop&env=staging`);
  }
  expect(nav.getByRole("link", { name: "Audit log" }).getAttribute("aria-current")).toBe("page");
});

test("only the current section is marked current, on that section's own URL", async () => {
  const page = await render(
    <Shell active="audit" project="shop" env="staging">
      <p>page</p>
    </Shell>,
    { url: "/audit?project=shop&env=staging" },
  );
  const nav = within(page.getAllByRole("navigation", { name: "Sections" })[0] ?? document.body);
  const current = nav
    .getAllByRole("link")
    .filter((link) => link.getAttribute("aria-current") === "page")
    .map((link) => link.textContent);
  expect(current).toEqual(["Audit log"]);
});
