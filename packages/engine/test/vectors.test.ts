import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { type Context, createRuleset, evaluate, type EvaluationResult } from "../src/index.ts";

interface Vector {
  name: string;
  flagKey: string;
  flags: unknown;
  segments: unknown;
  context: Context;
  expected: EvaluationResult;
}

const FIXTURES = join(import.meta.dirname, "../../../fixtures/evaluation");

function isVectorFile(value: unknown): value is { vectors: Vector[] } {
  return typeof value === "object" && value !== null && "vectors" in value;
}

function load(file: string): Vector[] {
  const parsed: unknown = JSON.parse(readFileSync(join(FIXTURES, file), "utf8"));
  if (!isVectorFile(parsed)) throw new Error(`${file} has no vectors`);
  return parsed.vectors;
}

const suites = readdirSync(FIXTURES)
  .filter((file) => file !== "buckets.json")
  .map((file) => ({ file, vectors: load(file) }));
const all = suites.flatMap((suite) => suite.vectors);

describe.each(suites)("$file", ({ vectors }) => {
  it.each(vectors)("$name", ({ flags, segments, flagKey, context, expected }) => {
    expect(evaluate(createRuleset({ flags, segments }), flagKey, context)).toEqual(expected);
  });
});

describe("the vectors as a set", () => {
  it("cover every reason and every error code", () => {
    const seen = new Set(
      all.map(({ expected: { reason } }) =>
        reason.kind === "ERROR" ? `ERROR:${reason.code}` : reason.kind,
      ),
    );
    expect([...seen].toSorted()).toEqual([
      "ERROR:FLAG_NOT_FOUND",
      "ERROR:INVALID_CONTEXT",
      "ERROR:MALFORMED_FLAG",
      "ERROR:MALFORMED_SEGMENT",
      "ERROR:PREREQUISITE_CYCLE",
      "ERROR:PREREQUISITE_DEPTH_EXCEEDED",
      "ERROR:SEGMENT_NOT_FOUND",
      "FALLTHROUGH",
      "OFF",
      "PREREQUISITE_FAILED",
      "RULE_MATCH",
      "TARGET_MATCH",
    ]);
  });
});
