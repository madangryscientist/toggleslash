import { describe, expect, test } from "vitest";
import { safeReturnPath, signInPath } from "./return-path.ts";

describe("safeReturnPath", () => {
  test.each(["/", "/keys", "/keys?project=shop&env=live", "/flags/checkout-v2?project=shop#x"])(
    "keeps %s",
    (path) => {
      expect(safeReturnPath(path)).toBe(path);
    },
  );

  test.each([
    "https://evil.test",
    "//evil.test",
    "/\\evil.test",
    "\\\\evil.test",
    "\\/evil.test",
    "/\t/evil.test",
    "/\n/evil.test",
    "/.//evil.test",
    "/..//evil.test",
    "/keys/..//evil.test",
    "//[",
    "/\\[",
    "javascript:alert(1)",
    "keys",
    "",
    null,
    undefined,
    42,
    ["/keys"],
  ])("refuses %j", (value) => {
    expect(safeReturnPath(value)).toBe("/");
  });
});

test("the paths to sign-in carry only a safe target", () => {
  expect(signInPath.to("/keys?a=1")).toBe("/sign-in?returnTo=%2Fkeys%3Fa%3D1");
  expect(signInPath.to("//evil.test")).toBe("/sign-in?returnTo=%2F");
  expect(signInPath.failed("/keys")).toBe("/sign-in?failed=true&returnTo=%2Fkeys");
  expect(signInPath.after("https://evil.test")).toBe("/");
});
