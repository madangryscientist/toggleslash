import { beforeEach, expect, test, vi } from "vitest";
import { loadRuleset } from "#/api/operator";
import { signOut } from "#/api/session";
import { render } from "#/test/render";
import { Route } from "../rulesets.$hash";

vi.mock("#/api/operator", () => ({ loadRuleset: vi.fn<typeof loadRuleset>() }));
vi.mock("#/api/session", () => ({ signOut: vi.fn<typeof signOut>() }));

const PAGE = { route: Route, path: "/rulesets/$hash" };
const HASH = "0".repeat(64);

beforeEach(() => {
  vi.resetAllMocks();
});

function notFound(message: string, flag: string | null = null) {
  vi.mocked(loadRuleset).mockResolvedValue({
    ok: true,
    project: "shop",
    env: "production",
    hash: HASH,
    flag,
    found: { ok: false, message },
  });
}

test("a hash the environment never served is a clear not-found, not an error page", async () => {
  notFound("The environment never served that hash.");
  const page = await render(PAGE, { url: `/rulesets/${HASH}?project=shop&env=production` });
  expect(page.getByText("No ruleset found for that hash")).toBeTruthy();
  expect(page.container.textContent).toContain("The environment never served that hash.");
  expect(page.queryByRole("heading", { name: "The server could not answer" })).toBeNull();
});

test("a flag the hash's ruleset lacks says so, not that the hash was never served", async () => {
  notFound("That ruleset has no such flag.", "nope");
  const page = await render(PAGE, {
    url: `/rulesets/${HASH}?project=shop&env=production&flag=nope`,
  });
  expect(page.container.textContent).toContain("That ruleset has no such flag.");
  expect(page.container.textContent).not.toContain("never served");
  expect(loadRuleset).toHaveBeenCalledWith({
    data: { project: "shop", env: "production", hash: HASH, flag: "nope" },
  });
});

test("without a project and an environment it says what is missing and asks the server nothing", async () => {
  const page = await render(PAGE, { url: `/rulesets/${HASH}` });
  expect(page.getByText("Which project and environment?")).toBeTruthy();
  expect(loadRuleset).not.toHaveBeenCalled();
});
