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
  test("only the .server.ts api files and the server-function file touch the connection", () => {
    const allowed = new Set([
      "api/client.server.ts",
      "api/detail.server.ts",
      "api/env.server.ts",
      "api/http.server.ts",
      "api/result.server.ts",
      "api/dashboard.ts",
      "api/operator.ts",
      "api/session-middleware.ts",
      "server.ts",
    ]);
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
        !file.startsWith("routes/") &&
        file !== "components/Shell.tsx",
    );
    // routes/ and the Shell's page copy are exempt here: they name the variable in help text (never a
    // value), and the test above checks they never import a `.server` file.
    // env.server.ts reads it; client.server.ts names it in its "is not set" message.
    expect(readers.toSorted()).toEqual(["api/client.server.ts", "api/env.server.ts"]);
  });
});

describe("the sign-in secrets stay on the server", () => {
  test("they are named only in server-side files and the type declaration", () => {
    const readers = files.filter(
      (file) =>
        /\.(ts|tsx)$/.test(file) &&
        !/\.test\./.test(file) &&
        /FEATURESLASH_(OPERATOR_PASSWORD|SESSION_SECRET)|\.sessionSecret|authSecrets/.test(
          readFileSync(join(SRC, file), "utf8"),
        ),
    );
    expect(readers.toSorted()).toEqual([
      "api/auth.server.ts",
      "api/env.server.ts",
      "api/session-middleware.ts",
      "server.ts",
      "worker-env.d.ts",
    ]);
  });
});

const source = (file: string): string => readFileSync(join(SRC, file), "utf8");

describe("every server function is behind the session middleware", () => {
  test("it is the first function middleware, on the one Start instance", () => {
    const start = source("start.ts");
    expect(start).toMatch(/functionMiddleware:\s*\[\s*requireSession\b/);
    expect(start).toMatch(/import \{ requireSession \} from "\.\/api\/session-middleware\.ts"/);
    expect(files.filter((f) => /\bcreateStart\(/.test(source(f))).filter(isCode)).toEqual([
      "start.ts",
    ]);
  });
});

function isCode(file: string): boolean {
  return /\.(ts|tsx)$/.test(file) && !/\.test\.|routeTree\.gen/.test(file);
}
