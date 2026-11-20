import { beforeEach, expect, test, vi } from "vitest";
import { loadAudit } from "#/api/operator";
import { signOut } from "#/api/session";
import { scope } from "#/test/fixtures";
import { render } from "#/test/render";
import { Route } from "../audit";

vi.mock("#/api/operator", () => ({ loadAudit: vi.fn<typeof loadAudit>() }));
vi.mock("#/api/session", () => ({ signOut: vi.fn<typeof signOut>() }));

beforeEach(() => {
  vi.resetAllMocks();
});

test("says when nothing matches the filters", async () => {
  vi.mocked(loadAudit).mockResolvedValue({
    ...scope(null),
    filters: { flag: "no-such-flag", environment: "", since: "" },
    cursor: null,
    page: { events: [], nextCursor: null },
  });
  const page = await render(
    { route: Route, path: "/audit" },
    { url: "/audit?project=shop&flag=no-such-flag" },
  );
  expect(page.getByText("No event matches these filters.")).toBeTruthy();
  expect(page.queryByRole("list", { name: "Audit trail" })).toBeNull();
  expect(loadAudit).toHaveBeenCalledWith({
    data: expect.objectContaining({ project: "shop", flag: "no-such-flag" }),
  });
});
