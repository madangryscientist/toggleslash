import { afterAll, describe, expect, test } from "bun:test";
import { chmodSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, join } from "node:path";
import { findDotnetSdk } from "./dotnet.ts";

const root = mkdtempSync(join(tmpdir(), "dotnet-test-"));
afterAll(() => rmSync(root, { recursive: true, force: true }));

/** A directory holding a fake `dotnet` that runs `body`. */
function fake(name: string, body: string): string {
  const dir = join(root, name);
  mkdirSync(dir);
  const file = join(dir, "dotnet");
  writeFileSync(file, `#!/bin/sh\n${body}\n`);
  chmodSync(file, 0o755);
  return dir;
}

describe("findDotnetSdk", () => {
  const broken = fake("broken", "echo 'No .NET SDKs were found' >&2; exit 1");
  const working = fake("working", "echo 10.0.402");
  const empty = join(root, "empty");
  mkdirSync(empty);

  test("skips a dotnet that fails and returns the next one with its version", () => {
    expect(findDotnetSdk([empty, broken, working].join(delimiter))).toEqual({
      path: join(working, "dotnet"),
      version: "10.0.402",
    });
  });

  test("returns null when none runs", () => {
    expect(findDotnetSdk([empty, broken].join(delimiter))).toBeNull();
    expect(findDotnetSdk("")).toBeNull();
  });
});
