import { describe, expect, test } from "vitest";
import type { FlagRow } from "#/api/types";
import { matches, parseTags } from "./text";

const flag: FlagRow = {
  key: "checkout-v2",
  name: "Checkout v2",
  description: "",
  tags: ["payments", "ui"],
  temporary: false,
  lastEvaluated: null,
  on: true,
  version: 1,
};

describe("matches", () => {
  test("an empty search matches everything", () => {
    expect(matches(flag, "")).toBe(true);
    expect(matches(flag, "   ")).toBe(true);
  });
  test("looks in key, name and tags, ignoring case", () => {
    expect(matches(flag, "CHECKOUT")).toBe(true);
    expect(matches(flag, "pay")).toBe(true);
    expect(matches(flag, "v2")).toBe(true);
  });
  test("every word must match", () => {
    expect(matches(flag, "checkout ui")).toBe(true);
    expect(matches(flag, "checkout search")).toBe(false);
  });
});

describe("parseTags", () => {
  test("splits on commas and spaces and drops repeats and blanks", () => {
    expect(parseTags("a, B  c,,A")).toEqual(["a", "b", "c"]);
    expect(parseTags("")).toEqual([]);
  });
});
