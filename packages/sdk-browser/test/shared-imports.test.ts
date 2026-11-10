import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { describe, expect, test } from "vitest";

const SDK_SRC = resolve(import.meta.dirname, "../../sdk/src");
const ENTRY = join(SDK_SRC, "shared.ts");

/** Every module specifier a file imports or re-exports from, including dynamic `import("...")`. */
function specifiers(source: string): string[] {
  const found: string[] = [];
  for (const match of source.matchAll(/(?:\bfrom\s*|\bimport\s*\(?\s*)["']([^"']+)["']/g)) {
    if (match[1] !== undefined) found.push(match[1]);
  }
  return found;
}

/** Follows every relative import from `entry`; returns each file reached and what each one imports. */
function walk(entry: string): Map<string, string[]> {
  const reached = new Map<string, string[]>();
  const queue = [entry];
  for (let file = queue.pop(); file !== undefined; file = queue.pop()) {
    if (reached.has(file)) continue;
    const imports = specifiers(readFileSync(file, "utf8"));
    reached.set(file, imports);
    for (const specifier of imports) {
      if (!specifier.startsWith(".")) continue;
      const target = resolve(dirname(file), specifier);
      queue.push(/\.[cm]?[jt]s$/.test(target) ? target : `${target}.ts`);
    }
  }
  return reached;
}

describe("@hackcove/toggleslash-sdk/shared", () => {
  const reached = walk(ENTRY);
  const names = [...reached.keys()].map((file) => file.slice(SDK_SRC.length + 1)).toSorted();

  test("reaches neither the engine, client.ts nor sync.ts", () => {
    const violations: string[] = [];
    for (const [file, imports] of reached) {
      for (const specifier of imports) {
        if (/toggleslash-engine|(^|\/)(client|sync)(\.ts)?$/.test(specifier))
          violations.push(`${file} imports ${specifier}`);
      }
    }
    expect(violations).toEqual([]);
    expect(names).not.toContain("client.ts");
    expect(names).not.toContain("sync.ts");
  });

  test("the browser package imports only the shared entry of the server SDK", () => {
    const src = resolve(import.meta.dirname, "../src");
    const imported = readdirSync(src)
      .filter((file) => file.endsWith(".ts"))
      .flatMap((file) => specifiers(readFileSync(join(src, file), "utf8")))
      .filter((specifier) => specifier.startsWith("@hackcove/"));
    expect(new Set(imported)).toEqual(new Set(["@hackcove/toggleslash-sdk/shared"]));
  });
});
