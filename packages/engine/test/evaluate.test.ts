import { describe, expect, it } from "vitest";
import { createRuleset, evaluate } from "../src/index.ts";

/** What the shared vectors cannot carry: a non-JSON value, and a ruleset that is not an object. */
const flag = (extra: object) => ({
  key: "f",
  variations: [false, true],
  on: true,
  offVariation: 0,
  targets: [],
  prerequisites: [],
  rules: [],
  fallthrough: { variation: 0 },
  salt: "s",
  version: 1,
  ...extra,
});
const gtFive = {
  rules: [{ clauses: [{ attribute: "n", op: "gt", values: [5] }], variation: 1 }],
};

describe("evaluate, beyond JSON", () => {
  it("treats a NaN attribute as a mismatch", () => {
    const ruleset = createRuleset({ flags: [flag(gtFive)], segments: [] });
    const context = { key: "k", attributes: { n: Number.NaN } };
    expect(evaluate(ruleset, "f", context).reason).toEqual({ kind: "FALLTHROUGH" });
  });

  it("treats a ruleset that is not an object as empty", () => {
    expect(evaluate(createRuleset(null), "f", { key: "k" }).reason).toEqual({
      kind: "ERROR",
      code: "FLAG_NOT_FOUND",
    });
  });
});
