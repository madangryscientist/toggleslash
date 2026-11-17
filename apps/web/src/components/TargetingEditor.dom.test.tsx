import { within } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { saveConfig } from "#/api/dashboard";
import type { SaveResult } from "#/api/types";
import type { Config } from "#/targeting/model";
import { checkout, checkoutConfig, production, shop } from "#/test/fixtures";
import { isDisabled, render, valueOf } from "#/test/render";
import { TargetingEditor } from "./TargetingEditor";

vi.mock("#/api/dashboard", () => ({ saveConfig: vi.fn<typeof saveConfig>() }));

const save = vi.mocked(saveConfig);

/** What `saveConfig` answers when the admin API refuses the config with `detail`. */
function refused(detail: string): SaveResult<Config> {
  return { ok: false, failure: { kind: "error", message: "The server refused it." }, detail };
}

const conflict: SaveResult<Config> = {
  ok: false,
  failure: { kind: "error", message: "The flag changed since you loaded it." },
  conflict: true,
  detail: "The flag changed since you loaded it.",
};

beforeEach(() => {
  save.mockReset();
});

async function editor() {
  const onSaved = vi.fn<() => Promise<void>>(async () => {});
  const onConflict = vi.fn<() => void>();
  const page = await render(
    <TargetingEditor
      project={shop}
      environment={production}
      flag={checkout}
      config={checkoutConfig}
      siblings={[]}
      onSaved={onSaved}
      onConflict={onConflict}
    />,
  );
  const rule = within(page.getByRole("region", { name: "Rule 1" }));
  const saveButton = page.getByRole("button", { name: "Save targeting" });
  return { page, user: page.user, rule, saveButton, onSaved, onConflict };
}

test("a client render is hydrated: the editor's controls are enabled from the first render", async () => {
  const { page, rule } = await editor();
  expect(isDisabled(page.getByRole("checkbox", { name: /Targeting is on/ }))).toBe(false);
  expect(isDisabled(rule.getByRole("textbox", { name: "Values" }))).toBe(false);
});

describe("an invalid entry is refused before it is sent", () => {
  test("a percentage that is not a number is marked at its rule", async () => {
    const { page, user, rule, saveButton } = await editor();
    const percent = rule.getByRole("textbox", { name: "true (%)" });
    await user.clear(percent);
    await user.type(percent, "lots");
    await user.click(saveButton);

    expect(rule.getByText(/A percentage from 0 to 100/)).toBeTruthy();
    expect(page.getByText("Fix the marked fields, then save.")).toBeTruthy();
    expect(save).not.toHaveBeenCalled();
  });

  test("an invalid context kind is marked at the target, the clause and the rollout", async () => {
    const { page, user, rule, saveButton } = await editor();
    await user.click(page.getByRole("button", { name: "Add a target for another kind" }));
    const targets = within(page.getByRole("region", { name: "Individual targets" }));
    await user.type(targets.getByRole("textbox", { name: "Context kind" }), "not valid");
    await user.type(rule.getByRole("textbox", { name: "Context kind" }), "multi");
    await user.type(rule.getByRole("textbox", { name: "Bucket by context kind" }), "a/b");
    await user.click(saveButton);

    expect(page.getAllByText(/A context kind uses letters/)).toHaveLength(3);
    expect(save).not.toHaveBeenCalled();
  });
});

describe("a save the server refuses", () => {
  test("a rollout that does not total 100% is shown at the rule, and the draft is kept", async () => {
    const { page, user, rule, saveButton, onSaved } = await editor();
    const first = rule.getByRole("textbox", { name: "true (%)" });
    await user.clear(first);
    await user.type(first, "30");
    expect(rule.getByText(/105%/)).toBeTruthy();

    save.mockResolvedValueOnce(
      refused("rules[0].rollout weights must sum to 100000, they sum to 105000."),
    );
    await user.click(saveButton);
    expect(await rule.findByText(/weights must sum to 100000/)).toBeTruthy();
    expect(page.getByText(/The server rejected the config/)).toBeTruthy();
    expect(valueOf(first)).toBe("30");

    // Fixing it clears the message, and the next save goes through.
    const second = rule.getByRole("textbox", { name: "false (%)" });
    await user.clear(second);
    await user.type(second, "70");
    expect(page.queryByText(/The server rejected the config/)).toBeNull();
    save.mockResolvedValueOnce({ ok: true, saved: checkoutConfig });
    await user.click(saveButton);
    expect(onSaved).toHaveBeenCalledOnce();
  });

  test("a clause with no attribute is marked at that clause", async () => {
    const { user, rule, saveButton } = await editor();
    const attribute = rule.getByRole("textbox", { name: "Attribute" });
    await user.clear(attribute);
    save.mockResolvedValueOnce(
      refused("rules[0].clauses[0].attribute must be a non-empty string."),
    );
    await user.click(saveButton);
    const message = await rule.findByText(/attribute must be a non-empty string/);
    // The message is the Attribute field's own line, in place of its hint, not one for the rule.
    expect(message.parentElement?.contains(attribute)).toBe(true);
    expect(rule.queryByText(/A name, or a path/)).toBeNull();
  });
});

test("a version conflict keeps the typing, says to reload, and refuses again", async () => {
  const { page, user, rule, saveButton, onConflict, onSaved } = await editor();
  const values = rule.getByRole("textbox", { name: "Values" });
  await user.clear(values);
  await user.type(values, "NL, DE, BE");
  save.mockResolvedValue(conflict);
  await user.click(saveButton);

  const warning = await page.findByRole("status");
  expect(warning.textContent).toContain("Someone else changed this flag");
  expect(warning.textContent).toContain("reload the page");
  expect(valueOf(values)).toBe("NL, DE, BE");
  expect(page.getByText("Version 3")).toBeTruthy();
  expect(onConflict).toHaveBeenCalledOnce();

  // More typing does not hide the warning, and saving again still refuses with the typing kept.
  await user.type(values, ", FR");
  expect(page.getByText("Someone else changed this flag")).toBeTruthy();
  await user.click(saveButton);
  expect(save).toHaveBeenCalledTimes(2);
  expect(save).toHaveBeenLastCalledWith({ data: expect.objectContaining({ version: 3 }) });
  expect(valueOf(values)).toBe("NL, DE, BE, FR");
  expect(page.getByText("Someone else changed this flag")).toBeTruthy();
  expect(onSaved).not.toHaveBeenCalled();
});
