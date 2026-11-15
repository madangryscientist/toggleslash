import { expect, test, vi } from "vitest";
import { signOut } from "#/api/session";
import type { FailureKind } from "#/api/types";
import { render } from "#/test/render";
import { ServerDown } from "./Shell";

vi.mock("#/api/session", () => ({ signOut: vi.fn<typeof signOut>() }));

test.each<[FailureKind, string, RegExp]>([
  ["unreachable", "The ToggleSlash server is not reachable", /TOGGLESLASH_API_URL/],
  ["unconfigured", "The dashboard is not configured", /address and the admin token/],
  ["unauthorized", "The server rejected the admin token", /TOGGLESLASH_ADMIN_TOKEN/],
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
