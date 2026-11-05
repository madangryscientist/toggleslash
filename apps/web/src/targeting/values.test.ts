import { describe, expect, test } from "vitest";
import { formatValue, formatValues, parseKeys, parseValues } from "./values.ts";
import { moveBy, removeAt, replaceAt } from "./list.ts";

describe("parseValues", () => {
  test("types bare tokens and keeps quoted ones as strings", () => {
    expect(parseValues('a, 12, true, "42", "x, y"')).toEqual(["a", 12, true, "42", "x, y"]);
  });
  test("drops blanks and reads an empty text as no values", () => {
    expect(parseValues("")).toEqual([]);
    expect(parseValues(" , a ,, ")).toEqual(["a"]);
  });
  test("a malformed quote stays text rather than throwing", () => {
    expect(parseValues('"abc')).toEqual(['"abc']);
  });
});

describe("formatValues", () => {
  test("quotes what would read back as another type", () => {
    expect(formatValue("42")).toBe('"42"');
    expect(formatValue("true")).toBe('"true"');
    expect(formatValue("a,b")).toBe('"a,b"');
    expect(formatValue("")).toBe('""');
    expect(formatValue("NL")).toBe("NL");
  });
  test("round-trips through parseValues", () => {
    const values = ["NL", 12, true, "42", "x, y", "false", " pad ", 1.5];
    expect(parseValues(formatValues(values))).toEqual(values);
  });
});

describe("parseKeys", () => {
  test("splits on lines and commas and trims", () => {
    expect(parseKeys("alice\n bob ,carol\n\n")).toEqual(["alice", "bob", "carol"]);
    expect(parseKeys("")).toEqual([]);
  });
});

describe("list edits", () => {
  test("replaceAt and removeAt leave the input alone", () => {
    const a = [1, 2, 3];
    expect(replaceAt(a, 1, 9)).toEqual([1, 9, 3]);
    expect(removeAt(a, 0)).toEqual([2, 3]);
    expect(a).toEqual([1, 2, 3]);
  });
  test("moveBy swaps with a neighbour and stops at the ends", () => {
    expect(moveBy([1, 2, 3], 1, -1)).toEqual([2, 1, 3]);
    expect(moveBy([1, 2, 3], 1, 1)).toEqual([1, 3, 2]);
    expect(moveBy([1, 2, 3], 0, -1)).toEqual([1, 2, 3]);
    expect(moveBy([1, 2, 3], 2, 1)).toEqual([1, 2, 3]);
  });
});
