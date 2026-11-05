import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { describe, expect, test } from "vitest";

const SRC = join(import.meta.dirname, "..");

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? walk(path) : [path];
  });
}

const files = walk(SRC).map((path) => relative(SRC, path));

describe("the admin token stays on the server", () => {
  test("only the two .server.ts files and the server-function file touch the connection", () => {
    const allowed = new Set(["api/client.server.ts", "api/env.server.ts", "api/dashboard.ts"]);
    const touching = files.filter((file) => {
      if (!/\.(ts|tsx)$/.test(file) || /\.test\.|routeTree\.gen/.test(file)) return false;
      const text = readFileSync(join(SRC, file), "utf8");
      return /cloudflare:workers|client\.server|env\.server|Bearer |process\.env/.test(text);
    });
    expect(touching.filter((file) => !allowed.has(file))).toEqual([]);
  });

  test("components and routes import the server functions, never the .server modules", () => {
    for (const file of files.filter((f) => /^(components|routes)\//.test(f))) {
      expect({ file, imports: /\.server/.test(readFileSync(join(SRC, file), "utf8")) }).toEqual({
        file,
        imports: false,
      });
    }
  });

  test("the token is named only in the two server-only api files", () => {
    const readers = files.filter(
      (file) =>
        /\.(ts|tsx)$/.test(file) &&
        !/\.test\./.test(file) &&
        /\[?["']?FEATURESLASH_ADMIN_TOKEN/.test(readFileSync(join(SRC, file), "utf8")) &&
        !file.startsWith("routes/"),
    );
    // routes/ is exempt here: it is checked above never to import a `.server` file.
    // env.server.ts reads it; client.server.ts names it in its "is not set" message.
    expect(readers.toSorted()).toEqual(["api/client.server.ts", "api/env.server.ts"]);
  });
});
