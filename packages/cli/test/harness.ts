import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach } from "vitest";
import type { Fetch } from "../src/api.ts";
import { run, type Deps } from "../src/cli.ts";

export const URL_BASE = "https://flags.example.test";
/** Distinctive enough that finding it in any output is a leak and never a coincidence. */
export const TOKEN = "fs-admin-token-do-not-print-0123456789";

export interface Result {
  code: number;
  stdout: string;
  stderr: string;
}

const dirs: string[] = [];
afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

/** A temp directory for XDG_CONFIG_HOME; `TMPDIR` is already a per-run scratch (scripts/test-tmp-scope.ts). */
export function tempDir(): string {
  const dir = mkdtempSync(join(tmpdir(), "fs-cli-"));
  dirs.push(dir);
  return dir;
}

export interface Options {
  fetch?: Fetch;
  env?: Record<string, string | undefined>;
  stdin?: string | null;
  configHome?: string;
}

/** Runs the CLI with a fake HTTP layer, a throwaway config dir and captured output. */
export async function cli(args: string[], options: Options = {}): Promise<Result> {
  let stdout = "";
  let stderr = "";
  const configHome = options.configHome ?? tempDir();
  const deps: Deps = {
    fetch:
      options.fetch ??
      (() => {
        throw new Error("this test must not touch the network");
      }),
    env: { XDG_CONFIG_HOME: configHome, ...options.env },
    home: configHome,
    readStdin: async () => options.stdin ?? null,
    stdout: (text) => {
      stdout += text;
    },
    stderr: (text) => {
      stderr += text;
    },
  };
  const code = await run(args, deps);
  return { code, stdout, stderr };
}

export const CONNECTED = { TOGGLESLASH_URL: URL_BASE, TOGGLESLASH_TOKEN: TOKEN };
