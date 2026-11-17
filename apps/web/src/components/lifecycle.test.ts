import { describe, expect, test } from "vitest";
import { stageOf } from "./lifecycle";

describe("stageOf", () => {
  test("gives each stage its label and tone", () => {
    expect(stageOf("new")).toEqual({ label: "New", tone: "info" });
    expect(stageOf("active")).toEqual({ label: "Active", tone: "positive" });
    expect(stageOf("inactive")).toEqual({ label: "Inactive", tone: "neutral" });
    expect(stageOf("archived")).toEqual({ label: "Archived", tone: "warn" });
  });
});
