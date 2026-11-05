import { describe, expect, test } from "vitest";
import { at, configFieldOf, metaFieldOf, under } from "./problems.ts";

describe("configFieldOf", () => {
  test("reads the path the server's message opens with", () => {
    expect(
      configFieldOf("rules[0].rollout weights must sum to 100000, they sum to 90000."),
    ).toEqual({
      path: "rules[0].rollout",
      message: "rules[0].rollout weights must sum to 100000, they sum to 90000.",
    });
    expect(configFieldOf("rules[2].clauses[1].attribute must be a non-empty string.")?.path).toBe(
      "rules[2].clauses[1].attribute",
    );
    expect(configFieldOf("offVariation must be an index into the variations.")?.path).toBe(
      "offVariation",
    );
    expect(configFieldOf("fallthrough must serve a variation or a rollout.")?.path).toBe(
      "fallthrough",
    );
  });
  test("a message with no path is the page's, not a field's", () => {
    expect(configFieldOf("version is stale.")).toBeNull();
    expect(configFieldOf("prerequisites would make a cycle: a -> b -> a.")).toBeNull();
    expect(configFieldOf("")).toBeNull();
  });
});

describe("metaFieldOf", () => {
  test("names the metadata field", () => {
    expect(metaFieldOf("name must not be empty.")?.path).toBe("name");
    expect(metaFieldOf("owner is too long.")?.path).toBe("owner");
    expect(metaFieldOf("every tag must be lowercase.")?.path).toBe("tags");
    expect(metaFieldOf("a flag takes at most 20 tags.")?.path).toBe("tags");
    expect(metaFieldOf("something else")).toBeNull();
  });
});

describe("under and at", () => {
  const problems = [
    { path: "rules[0]", message: "a" },
    { path: "rules[0].rollout", message: "b" },
    { path: "rules[1].rollout", message: "c" },
    { path: "rules[10]", message: "d" },
  ];
  test("under takes the path and what is below it, not a longer index", () => {
    expect(under(problems, "rules[0]").map((p) => p.message)).toEqual(["a", "b"]);
    expect(under(problems, "rules[1]").map((p) => p.message)).toEqual(["c"]);
  });
  test("at takes exactly the path", () => {
    expect(at(problems, "rules[0].rollout")?.message).toBe("b");
    expect(at(problems, "rules")).toBeUndefined();
  });
});
