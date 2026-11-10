import { describe, expect, test } from "vitest";
import type { Config } from "./model.ts";
import {
  type ServeDraft,
  emptyClause,
  isDirty,
  percentOf,
  rolloutTotal,
  switchServe,
  toDraft,
  weightOf,
} from "./draft.ts";
import { toWire } from "./wire.ts";

const config: Config = {
  on: true,
  offVariation: 1,
  targets: [{ variation: 0, contextKeys: ["alice", "bob"] }],
  prerequisites: [{ flagKey: "base", variation: 0 }],
  rules: [
    {
      clauses: [{ op: "in", attribute: "country", values: ["NL", "42", 7], negate: false }],
      serve: {
        kind: "rollout",
        entries: [
          { variation: 0, weight: 25_000 },
          { variation: 1, weight: 75_000 },
        ],
      },
    },
  ],
  fallthrough: { kind: "variation", variation: 1 },
  version: 3,
};

describe("weightOf and percentOf", () => {
  test("read percents with up to three decimals", () => {
    expect(weightOf("25")).toBe(25_000);
    expect(weightOf(" 33.333 ")).toBe(33_333);
    expect(weightOf("0")).toBe(0);
    expect(weightOf("100")).toBe(100_000);
  });
  test("refuse anything else", () => {
    for (const bad of ["", "abc", "-1", "100.001", "101", "1.2345", "1e2", "1,5"]) {
      expect(weightOf(bad)).toBeNull();
    }
  });
  test("percentOf inverts weightOf", () => {
    expect(percentOf(25_000)).toBe("25");
    expect(percentOf(33_333)).toBe("33.333");
    expect(weightOf(percentOf(33_333))).toBe(33_333);
  });
});

describe("toDraft and toWire", () => {
  test("a draft of a config sends the same config back", () => {
    const sent = toWire(toDraft(config, 2));
    expect(sent.ok).toBe(true);
    if (!sent.ok) return;
    expect(sent.wire).toEqual({
      on: true,
      offVariation: 1,
      targets: [{ variation: 0, contextKeys: ["alice", "bob"] }],
      prerequisites: [{ flagKey: "base", variation: 0 }],
      rules: [
        {
          clauses: [{ op: "in", attribute: "country", values: ["NL", "42", 7] }],
          rollout: {
            variations: [
              { variation: 0, weight: 25_000 },
              { variation: 1, weight: 75_000 },
            ],
          },
        },
      ],
      fallthrough: { variation: 1 },
    });
  });
  test("a rollout gets a line for every variation and leaves zero weights off the wire", () => {
    const draft = toDraft(config, 3);
    const rule = draft.rules[0];
    expect(rule?.serve.kind === "rollout" && rule.serve.entries.map((e) => e.percent)).toEqual([
      "25",
      "75",
      "0",
    ]);
    const sent = toWire(draft);
    expect(JSON.stringify(sent)).not.toContain('"weight":0');
  });
  test("a percentage that is not a number is a problem at its row", () => {
    const draft = toDraft(config, 2);
    const rule = draft.rules[0];
    if (rule?.serve.kind !== "rollout") throw new Error("fixture");
    rule.serve.entries[1] = { variation: 1, percent: "lots" };
    const sent = toWire(draft);
    expect(sent.ok).toBe(false);
    if (sent.ok) return;
    expect(sent.problems.map((p) => p.path)).toEqual(["rules[0].rollout.entries[1]"]);
  });
  test("a segment clause sends no attribute and a negate only when set", () => {
    const draft = toDraft(config, 2);
    draft.rules[0]?.clauses.push({
      op: "segmentMatch",
      attribute: "",
      values: "beta",
      negate: true,
    });
    const sent = toWire(draft);
    if (!sent.ok) throw new Error("fixture");
    expect(JSON.stringify(sent.wire.rules[0])).toContain(
      JSON.stringify({ op: "segmentMatch", values: ["beta"], negate: true }),
    );
  });
  test("segment keys stay strings, so 007 is not 7", () => {
    const draft = toDraft(config, 2);
    draft.rules[0]?.clauses.push({
      op: "segmentMatch",
      attribute: "",
      values: "007, true",
      negate: false,
    });
    const sent = toWire(draft);
    if (!sent.ok) throw new Error("fixture");
    expect(JSON.stringify(sent.wire.rules[0])).toContain('"values":["007","true"]');
  });
  test("a version typed bare under a semver op is sent as a string, and stays a number under gte", () => {
    const draft = toDraft(config, 2);
    draft.rules[0]?.clauses.push(
      { op: "semverGte", attribute: "appVersion", values: "2.1, 1.2.3", negate: false },
      { op: "gte", attribute: "age", values: "2.1, 1.2.3", negate: false },
    );
    const sent = toWire(draft);
    if (!sent.ok) throw new Error("fixture");
    const rule = JSON.stringify(sent.wire.rules[0]);
    expect(rule).toContain('"op":"semverGte","attribute":"appVersion","values":["2.1","1.2.3"]');
    expect(rule).toContain('"op":"gte","attribute":"age","values":[2.1,"1.2.3"]');
  });
  test("a date typed under before goes out as one string, and a bare number as a number", () => {
    const draft = toDraft(config, 2);
    draft.rules[0]?.clauses.push({
      op: "before",
      attribute: "signedUp",
      values: "2026-01-01T00:00:00Z, 1767225600000",
      negate: false,
    });
    const sent = toWire(draft);
    if (!sent.ok) throw new Error("fixture");
    expect(JSON.stringify(sent.wire.rules[0])).toContain(
      '"op":"before","attribute":"signedUp","values":["2026-01-01T00:00:00Z",1767225600000]',
    );
  });
  test("a saved semver clause survives draft to wire unchanged", () => {
    const saved: Config = {
      ...config,
      rules: [
        {
          clauses: [
            { op: "semverGte", attribute: "appVersion", values: ["2.1", "1.2.3"], negate: true },
          ],
          serve: { kind: "variation", variation: 0 },
        },
      ],
    };
    const sent = toWire(toDraft(saved, 3));
    if (!sent.ok) throw new Error("fixture");
    expect(sent.wire.rules[0]).toEqual({
      clauses: [
        { op: "semverGte", attribute: "appVersion", values: ["2.1", "1.2.3"], negate: true },
      ],
      variation: 0,
    });
  });
});

describe("switchServe", () => {
  test("a variation becomes a rollout of 100 on it", () => {
    expect(switchServe({ kind: "variation", variation: 1 }, "rollout", 3)).toEqual({
      kind: "rollout",
      entries: [
        { variation: 0, percent: "0" },
        { variation: 1, percent: "100" },
        { variation: 2, percent: "0" },
      ],
    });
  });
  test("a rollout becomes its biggest variation", () => {
    const rollout: ServeDraft = {
      kind: "rollout",
      entries: [
        { variation: 0, percent: "30" },
        { variation: 1, percent: "70" },
      ],
    };
    expect(switchServe(rollout, "variation", 2)).toEqual({ kind: "variation", variation: 1 });
  });
  test("switching to the same kind changes nothing", () => {
    const serve: ServeDraft = { kind: "variation", variation: 0 };
    expect(switchServe(serve, "variation", 2)).toBe(serve);
  });
});

describe("isDirty, rolloutTotal, emptyClause", () => {
  test("isDirty compares by value", () => {
    expect(isDirty(toDraft(config, 2), toDraft(config, 2))).toBe(false);
    const changed = toDraft(config, 2);
    changed.on = false;
    expect(isDirty(toDraft(config, 2), changed)).toBe(true);
  });
  test("rolloutTotal counts unreadable percents as 0", () => {
    expect(
      rolloutTotal([
        { variation: 0, percent: "40" },
        { variation: 1, percent: "x" },
      ]),
    ).toBe(40_000);
  });
  test("a new clause is an empty `in`", () => {
    expect(emptyClause()).toEqual({ op: "in", attribute: "", values: "", negate: false });
  });
});
