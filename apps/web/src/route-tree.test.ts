import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// The tsr CLI (run by `typecheck`) writes routeTreeFileFooter, and the Vite plugin writes the
// committed form. If the two disagree, every gate rewrites routeTree.gen.ts and dirties the tree.
describe("routeTree.gen.ts", () => {
  it("ends with the footer tsr.config.json writes", () => {
    const config: unknown = JSON.parse(readFileSync("tsr.config.json", "utf8"));
    const footers =
      typeof config === "object" && config !== null && "routeTreeFileFooter" in config
        ? config.routeTreeFileFooter
        : undefined;
    const footer = Array.isArray(footers) ? footers[0] : undefined;
    if (typeof footer !== "string") {
      throw new Error("tsr.config.json has no routeTreeFileFooter string entry");
    }
    const generated = readFileSync("src/routeTree.gen.ts", "utf8").trimEnd();

    expect(
      generated.endsWith(footer),
      "routeTree.gen.ts does not end with routeTreeFileFooter in tsr.config.json: update tsr.config.json to match what the Vite plugin writes",
    ).toBe(true);
  });
});
