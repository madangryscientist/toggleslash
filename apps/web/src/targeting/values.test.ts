import { describe, expect, test } from "vitest";
import {
  formatKindKeys,
  formatValue,
  formatValues,
  isValidKind,
  parseKeys,
  parseKindKeys,
  parseValues,
} from "./values.ts";
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

describe("isValidKind", () => {
  test("accepts letters, digits, dot, underscore and hyphen", () => {
    for (const kind of ["org", "Org-1", "a.b_c", "user"]) expect(isValidKind(kind)).toBe(true);
  });
  test("refuses blank, other characters and the reserved words", () => {
    for (const kind of ["", "a b", "a:b", "a/b", "kind", "multi"]) {
      expect(isValidKind(kind)).toBe(false);
    }
  });
});

describe("parseKindKeys", () => {
  test("groups by kind in first-seen order, values in line order", () => {
    expect(parseKindKeys("org:acme\ndevice:d1\norg:globex")).toEqual({
      ok: true,
      entries: [
        { contextKind: "org", values: ["acme", "globex"] },
        { contextKind: "device", values: ["d1"] },
      ],
    });
  });
  test("splits at the first colon, so a key may hold one", () => {
    expect(parseKindKeys("org:a:b")).toEqual({
      ok: true,
      entries: [{ contextKind: "org", values: ["a:b"] }],
    });
  });
  test("skips blank lines, trims, and does not split on commas", () => {
    expect(parseKindKeys("\n  org : acme, globex \n\n")).toEqual({
      ok: true,
      entries: [{ contextKind: "org", values: ["acme, globex"] }],
    });
  });
  test("names the line of a missing colon, an empty key, an invalid kind and user", () => {
    expect(parseKindKeys("org:a\nacme")).toMatchObject({ ok: false });
    for (const [text, line] of [
      ["org:a\nacme", "Line 2"],
      ["org:", "Line 1"],
      ["\nbad kind:a", "Line 2"],
      [":a", "Line 1"],
      ["multi:a", "Line 1"],
      ["user:alice", "Line 1"],
    ] as const) {
      const result = parseKindKeys(text);
      expect(result.ok ? "" : result.message.slice(0, line.length + 1)).toBe(`${line}:`);
    }
  });
  test("an empty text is no entries", () => {
    expect(parseKindKeys("")).toEqual({ ok: true, entries: [] });
  });
  test("formatKindKeys writes it back", () => {
    expect(formatKindKeys([{ contextKind: "org", values: ["acme", "globex"] }])).toBe(
      "org:acme\norg:globex",
    );
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
