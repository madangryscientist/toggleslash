import { within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { createSegment, deleteSegment, loadSegments, saveSegment } from "#/api/operator";
import { signOut } from "#/api/session";
import { betaTesters, scope, staff, staging } from "#/test/fixtures";
import { render, valueOf } from "#/test/render";
import { Route } from "../segments";

vi.mock("#/api/operator", () => ({
  loadSegments: vi.fn<typeof loadSegments>(),
  saveSegment: vi.fn<typeof saveSegment>(),
  createSegment: vi.fn<typeof createSegment>(),
  deleteSegment: vi.fn<typeof deleteSegment>(),
}));
vi.mock("#/api/session", () => ({ signOut: vi.fn<typeof signOut>() }));

const PAGE = { route: Route, path: "/segments" };
const URL = "/segments?project=shop&env=production";

beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(loadSegments).mockResolvedValue({ ...scope(), segments: [betaTesters, staff] });
});

async function segments(url = URL) {
  const page = await render(PAGE, { url });
  return { page, user: page.user };
}

async function edit(key: string) {
  const { page, user } = await segments();
  await user.click(page.getByRole("button", { name: `Edit ${key}` }));
  const dialog = within(page.getByRole("dialog", { name: `Edit ${key}` }));
  return { page, user, dialog };
}

test("lists segments with their counts", async () => {
  const { page } = await segments();
  expect(
    page.getByRole("row", { name: "beta-testers 2 contexts 1 context 1 rule Edit Delete" }),
  ).toBeTruthy();
  expect(
    page.getByRole("row", { name: "staff 0 contexts 0 contexts 0 rules Edit Delete" }),
  ).toBeTruthy();
});

test("an environment with none says so, and offers to create the first", async () => {
  vi.mocked(loadSegments).mockResolvedValue({ ...scope(staging), segments: [] });
  const { page, user } = await segments("/segments?project=shop&env=staging");
  expect(page.getByText("No segments yet")).toBeTruthy();
  await user.click(page.getByRole("button", { name: "Create the first segment" }));
  expect(page.getByRole("dialog", { name: "New segment" })).toBeTruthy();
});

describe("creating a segment", () => {
  test("a missing key is refused before asking, a taken key shows the server's message, and Cancel adds nothing", async () => {
    const { page, user } = await segments();
    await user.click(page.getByRole("button", { name: "New segment" }));
    const dialog = within(page.getByRole("dialog", { name: "New segment" }));
    await user.click(dialog.getByRole("button", { name: "Create segment" }));
    const alerts = dialog.getAllByRole("alert").map((alert) => alert.textContent);
    expect(alerts).toContain(
      "Give the segment a key: lowercase letters, digits, - and _, up to 64.",
    );
    expect(createSegment).not.toHaveBeenCalled();

    vi.mocked(createSegment).mockResolvedValue({
      ok: false,
      failure: { kind: "error", message: "The server refused it." },
      conflict: true,
      detail: 'Segment "staff" already exists in environment "production".',
    });
    await user.type(dialog.getByRole("textbox", { name: /^Key/ }), "staff");
    await user.click(dialog.getByRole("button", { name: "Create segment" }));
    expect(
      await dialog.findByText('Segment "staff" already exists in environment "production".'),
    ).toBeTruthy();

    await user.click(dialog.getByRole("button", { name: "Cancel" }));
    expect(page.queryByRole("dialog")).toBeNull();
    expect(page.getAllByRole("row")).toHaveLength(3);
  });
});

describe("editing a segment", () => {
  test("rules that are not JSON are refused before asking, and the server's refusal is shown", async () => {
    const { user, dialog } = await edit("staff");
    const rules = dialog.getByRole("textbox", { name: "Rules" });
    await user.clear(rules);
    await user.type(rules, "not json");
    await user.click(dialog.getByRole("button", { name: "Save segment" }));
    expect(dialog.getAllByText("The rules must be a JSON array.").length).toBeGreaterThan(0);
    expect(saveSegment).not.toHaveBeenCalled();

    vi.mocked(saveSegment).mockResolvedValue({
      ok: false,
      failure: { kind: "error", message: "The server refused it." },
      detail: "rules[0].clauses must be a list.",
    });
    await user.clear(rules);
    // `{` and `[` open a key descriptor in `user.type`; doubled, they are typed as they are.
    await user.type(rules, '[[{{"nope": 1}]');
    await user.click(dialog.getByRole("button", { name: "Save segment" }));
    expect(await dialog.findByText("rules[0].clauses must be a list.")).toBeTruthy();
  });

  test("a line of another kind without its kind names itself, and nothing is sent", async () => {
    const { user, dialog } = await edit("beta-testers");
    const kinds = dialog.getByRole("textbox", { name: "Included, other kinds" });
    await user.type(kinds, "org:acme{Enter}acme");
    await user.click(dialog.getByRole("button", { name: "Save segment" }));
    const error = dialog.getByRole("alert");
    expect(error.textContent).toContain("Line 2");
    expect(error.textContent).toContain("Included, other kinds");
    expect(saveSegment).not.toHaveBeenCalled();
  });

  test("a version conflict keeps the typing and says to reload", async () => {
    vi.mocked(saveSegment).mockResolvedValue({
      ok: false,
      failure: { kind: "error", message: "The segment changed since you loaded it." },
      conflict: true,
    });
    const { user, dialog } = await edit("beta-testers");
    const included = dialog.getByRole("textbox", { name: "Always included" });
    expect(valueOf(included)).toBe("alice\nbob");
    await user.clear(included);
    await user.type(included, "alice{Enter}carol");
    await user.click(dialog.getByRole("button", { name: "Save segment" }));

    const error = await dialog.findByRole("alert");
    expect(error.textContent).toContain("Someone else changed this segment");
    expect(error.textContent).toContain("reload the page");
    expect(valueOf(included)).toBe("alice\ncarol");
    expect(saveSegment).toHaveBeenCalledWith({
      data: expect.objectContaining({ segment: "beta-testers", version: 1 }),
    });
  });
});

describe("deleting a segment", () => {
  test("Cancel keeps the segment and deletes nothing", async () => {
    const { page, user } = await segments();
    await user.click(page.getByRole("button", { name: "Delete staff" }));
    const dialog = within(page.getByRole("dialog", { name: "Delete staff?" }));
    await user.click(dialog.getByRole("button", { name: "Cancel" }));
    expect(page.queryByRole("dialog")).toBeNull();
    expect(page.getByRole("button", { name: "Edit staff" })).toBeTruthy();
    expect(deleteSegment).not.toHaveBeenCalled();
  });

  test("one a flag rule names is refused, and the server says which flag", async () => {
    const message =
      'Segment "beta-testers" is still used by the rules of: search-boost. Remove those rules first.';
    vi.mocked(deleteSegment).mockResolvedValue({
      ok: false,
      failure: { kind: "error", message },
      conflict: true,
    });
    const { page, user } = await segments();
    await user.click(page.getByRole("button", { name: "Delete beta-testers" }));
    const dialog = within(page.getByRole("dialog", { name: "Delete beta-testers?" }));
    await user.click(dialog.getByRole("button", { name: "Delete" }));
    expect(await page.findByText(message)).toBeTruthy();
    expect(page.getByRole("button", { name: "Edit beta-testers" })).toBeTruthy();
  });
});
