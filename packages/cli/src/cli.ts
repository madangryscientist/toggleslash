import { describeEngine } from "@hackcove/featureslash-engine";
import pkg from "../package.json" with { type: "json" };

const HELP = `featureslash: feature flags from the terminal.

Usage:
  featureslash --version   print the version
  featureslash --help      print this help

There are no flag commands yet.
`;

/** Runs the CLI for `args` and returns the exit code: 0 ok, 2 a usage error. */
export async function run(args: readonly string[]): Promise<number> {
  const [first] = args;
  if (first === "--version" || first === "-v") {
    process.stdout.write(`featureslash ${pkg.version} (${describeEngine()})\n`);
    return 0;
  }
  if (first === undefined || first === "--help" || first === "-h") {
    process.stdout.write(HELP);
    return 0;
  }
  process.stderr.write(`featureslash: unknown argument "${first}". Try --help.\n`);
  return 2;
}
