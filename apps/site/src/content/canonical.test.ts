import { describe, expect, it } from "vitest";
import { canonicalUrl } from "./canonical.ts";
import { PAGES } from "./pages.ts";

describe("canonicalUrl", () => {
  it("puts every page on the product domain", () => {
    expect(canonicalUrl("/")).toBe("https://featureslash.dev/");
    expect(canonicalUrl("/docs")).toBe("https://featureslash.dev/docs");
  });

  it("has a canonical URL for each page of the site", () => {
    for (const page of PAGES)
      expect(canonicalUrl(page.to)).toMatch(/^https:\/\/featureslash\.dev\//);
  });
});
