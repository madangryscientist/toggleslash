import { describe, expect, test } from "vitest";
import { stageOf } from "./lifecycle";

describe("stageOf", () => {
  test("gives each stage its label and tone", () => {
    expect(stageOf("new")).toEqual({ label: "New", tone: "blue" });
    expect(stageOf("active")).toEqual({ label: "Active", tone: "emerald" });
    expect(stageOf("inactive")).toEqual({ label: "Inactive", tone: "slate" });
    expect(stageOf("archived")).toEqual({ label: "Archived", tone: "amber" });
  });
});
