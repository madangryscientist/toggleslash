import { homedir } from "node:os";
import { parseArgs } from "node:util";
import { describeEngine } from "@hackcove/featureslash-engine";
import pkg from "../package.json" with { type: "json" };
import { createApi, type Fetch } from "./api.ts";
import type { Command, Context } from "./command.ts";
import { COMMANDS } from "./commands/index.ts";
import { resolveConnection, type ConfigEnv } from "./config.ts";
import { CliError, EXIT_FAILURE, EXIT_OK, UsageError } from "./errors.ts";

/** Everything the CLI takes from the outside. Tests pass fakes; `run(args)` uses the process. */
export interface Deps {
  /** The HTTP layer. */
  fetch: Fetch;
  env: Readonly<Record<string, string | undefined>>;
  home: string;
  /** Stdin as text, or `null` when it is a terminal. */
  readStdin: () => Promise<string | null>;
  stdout: (text: string) => void;
  stderr: (text: string) => void;
}

const processDeps: Deps = {
  fetch: (input, init) => fetch(input, init),
  env: process.env,
  home: homedir(),
  readStdin: async () => (process.stdin.isTTY ? null : Bun.stdin.text()),
  stdout: (text) => void process.stdout.write(text),
  stderr: (text) => void process.stderr.write(text),
};

function commandLines(): string {
  const width = Math.max(...COMMANDS.map((c) => `${c.name} ${c.usage}`.trimEnd().length));
  return COMMANDS.map(
    (c) => `  featureslash ${`${c.name} ${c.usage}`.trimEnd().padEnd(width)}  ${c.summary}`,
  ).join("\n");
}

const HELP = `featureslash: feature flags from the terminal.

Usage:
  featureslash --version   print the version
  featureslash --help      print this help

Commands (every command that calls the server also takes --url <url> and --json):
${commandLines()}

Connection:
  The server URL is --url, else FEATURESLASH_URL, else \`config set-url\`.
  The admin token is FEATURESLASH_TOKEN, else \`config set-token\`. An environment variable wins over the
  config file, which is $XDG_CONFIG_HOME/featureslash/config.json (default ~/.config/...), mode 600.
  The token is never a flag, so it stays out of your shell history and the process list.

Output:
  --json prints what the server sent, as JSON, instead of a table; it is on every command that calls the server.

Exit codes:
  0  success
  1  the server or the network failed (unreachable, a 4xx or 5xx answer, a rejected token, an unreadable reply)
  2  a usage error (unknown command, missing or invalid argument, no URL or token configured)
`;

/** Hides every secret it was told about, in everything it writes. */
function guardedWriter(
  write: (text: string) => void,
  secrets: Set<string>,
): (text: string) => void {
  return (text) => {
    let safe = text;
    // A value under 8 characters is not masked: it would also mangle ordinary words and JSON in the output.
    for (const secret of secrets) if (secret.length >= 8) safe = safe.replaceAll(secret, "***");
    write(safe);
  };
}

function findCommand(args: readonly string[]): Command {
  const [group, action] = args;
  const known = COMMANDS.find((c) => c.name === `${group} ${action}`);
  if (known !== undefined) return known;
  const groupActions = COMMANDS.filter((c) => c.name.startsWith(`${group} `)).map((c) =>
    c.name.slice(`${group} `.length),
  );
  if (groupActions.length > 0) {
    throw new UsageError(
      action === undefined
        ? `${group} needs one of: ${groupActions.join(", ")}. Try --help.`
        : `unknown command "${group} ${action}"; ${group} takes: ${groupActions.join(", ")}. Try --help.`,
    );
  }
  throw new UsageError(`unknown argument "${group}". Try --help.`);
}

async function dispatch(args: readonly string[], deps: Deps, secrets: Set<string>): Promise<void> {
  const out = guardedWriter(deps.stdout, secrets);
  const err = guardedWriter(deps.stderr, secrets);
  const command = findCommand(args);
  const where: ConfigEnv = { env: deps.env, home: deps.home };

  let parsed;
  try {
    parsed = parseArgs({
      args: args.slice(2),
      allowPositionals: true,
      strict: true,
      options: {
        ...command.options,
        ...(command.api ? { json: { type: "boolean" }, url: { type: "string" } } : {}),
      },
    });
  } catch (error) {
    throw new UsageError(
      `${command.name}: ${error instanceof Error ? error.message : "bad arguments"}. Try --help.`,
    );
  }
  const { values, positionals } = parsed;
  if (positionals.length !== command.required.length) {
    const wanted = command.required.join(", ");
    throw new UsageError(
      `${command.name} takes ${wanted === "" ? "no arguments" : wanted}; got ${positionals.length}. Try --help.`,
    );
  }

  const ctx: Context = {
    where,
    api(urlFlag) {
      const connection = resolveConnection(where, urlFlag);
      secrets.add(connection.token);
      return createApi(connection, deps.fetch);
    },
    readStdin: deps.readStdin,
    out,
    err,
    hide: (secret) => void secrets.add(secret),
  };
  await command.run({ values, positionals }, ctx);
}

/**
 * Runs the CLI for `args` and returns the exit code: 0 ok, 1 the server or the network failed, 2 a usage error.
 * Output goes through `deps` (the process's stdout and stderr by default), and the admin token is
 * replaced with `***` in all of it.
 */
export async function run(args: readonly string[], deps: Deps = processDeps): Promise<number> {
  const secrets = new Set<string>();
  const stdout = guardedWriter(deps.stdout, secrets);
  const stderr = guardedWriter(deps.stderr, secrets);
  const [first] = args;
  if (first === "--version" || first === "-v") {
    stdout(`featureslash ${pkg.version} (${describeEngine()})\n`);
    return EXIT_OK;
  }
  if (first === undefined || args.some((a) => a === "--help" || a === "-h")) {
    stdout(HELP);
    return EXIT_OK;
  }
  try {
    await dispatch(args, deps, secrets);
    return EXIT_OK;
  } catch (error) {
    if (error instanceof CliError) {
      stderr(`featureslash: ${error.message}\n`);
      return error.exitCode;
    }
    // A bug or a full disk: still one line, still no token.
    stderr(
      `featureslash: unexpected error: ${error instanceof Error ? error.message : "unknown"}\n`,
    );
    return EXIT_FAILURE;
  }
}
