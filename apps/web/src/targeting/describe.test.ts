import { describe, expect, it } from "vitest";
import { describeClause, describeServe, variationLabel } from "./describe.ts";

const variations = ["true", "false"];

describe("variationLabel", () => {
  it("shows the index with its value", () => {
    expect(variationLabel(1, variations)).toBe("1 (false)");
  });
  it("says so when the ruleset has no such variation", () => {
    expect(variationLabel(5, variations)).toBe("5 (missing)");
  });
});

describe("describeServe", () => {
  it("names a fixed variation", () => {
    expect(describeServe({ kind: "variation", variation: 0 }, variations)).toBe("serve 0 (true)");
  });
  it("gives a rollout's shares as percentages", () => {
    const serve = {
      kind: "rollout" as const,
      entries: [
        { variation: 0, weight: 25_500 },
        { variation: 1, weight: 74_500 },
      ],
    };
    expect(describeServe(serve, variations)).toBe("roll out 25.5% 0 (true), 74.5% 1 (false)");
  });
});

describe("describeClause", () => {
  it("reads an attribute clause and a negated one", () => {
    const c = { op: "in", attribute: "country", values: ["NL", "BE"], negate: false };
    expect(describeClause(c)).toBe('country in "NL", "BE"');
    expect(describeClause({ ...c, negate: true })).toBe('country not in "NL", "BE"');
  });
  it("reads a segment clause by the segment's key", () => {
    const c = { op: "segmentMatch", attribute: "", values: ["beta"], negate: false };
    expect(describeClause(c)).toBe("in segment beta");
  });
});
