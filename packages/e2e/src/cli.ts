import { join } from "node:path";
import { REPO_ROOT } from "./dotnet.ts";

const CLI_ENTRY = join(REPO_ROOT, "packages", "cli", "src", "index.ts");

export interface CliResult {
  code: number;
  stdout: string;
  stderr: string;
}

/**
 * The real CLI, as a subprocess (`bun packages/cli/src/index.ts`), pointed at one server by the
 * environment. `home` holds its config directory, so a run never reads or writes the developer's own.
 */
export function createCli(options: { url: string; token: string; home: string }) {
  async function run(args: string[]): Promise<CliResult> {
    const child = Bun.spawn(["bun", CLI_ENTRY, ...args], {
      cwd: REPO_ROOT,
      env: {
        ...process.env,
        HOME: options.home,
        XDG_CONFIG_HOME: join(options.home, "config"),
        FEATURESLASH_URL: options.url,
        FEATURESLASH_TOKEN: options.token,
      },
      stdin: "ignore",
      stdout: "pipe",
      stderr: "pipe",
    });
    const [stdout, stderr, code] = await Promise.all([
      child.stdout.text(),
      child.stderr.text(),
      child.exited,
    ]);
    return { code, stdout, stderr };
  }

  /** Runs a command that must exit 0 and prints JSON (`--json` is added). */
  async function json(args: string[]): Promise<unknown> {
    const result = await run([...args, "--json"]);
    if (result.code !== 0) {
      throw new Error(`featureslash ${args.join(" ")} exited ${result.code}: ${result.stderr}`);
    }
    return JSON.parse(result.stdout);
  }

  return { run, json };
}

export type Cli = ReturnType<typeof createCli>;
