/**
 * `bun run conformance -- --service "<command>"` or `-- --service-url <url>`: runs every scenario and vector
 * against an SDK's test service. Exit 0 when nothing failed, 1 when something did, 2 on a usage error.
 */
import { parseArgs } from "node:util";
import { failed, runAll, summaryLine } from "./run.ts";
import { type Started, startService } from "./service.ts";

const USAGE =
  'usage: bun run conformance -- (--service "<command>" | --service-url <url>) [--only <text>]\n';

function print(line: string): void {
  process.stdout.write(`${line}\n`);
}

interface Args {
  command: string | undefined;
  given: string | undefined;
  only: string | undefined;
}

/** The arguments, or null after printing the usage. */
function readArgs(): Args | null {
  try {
    const { values } = parseArgs({
      options: {
        service: { type: "string" },
        "service-url": { type: "string" },
        only: { type: "string" },
      },
    });
    const args = { command: values.service, given: values["service-url"], only: values.only };
    if ((args.command === undefined) !== (args.given === undefined)) return args;
    process.stderr.write(USAGE);
  } catch (error) {
    process.stderr.write(`${String(error)}\n${USAGE}`);
  }
  return null;
}

async function main(): Promise<number> {
  const args = readArgs();
  if (args === null) return 2;
  const { command, given, only } = args;
  let started: Started | null = null;
  try {
    if (command !== undefined) started = await startService(command);
    const serviceUrl = started?.url ?? given ?? "";
    const summary = await runAll({ serviceUrl, only, print });
    print(summaryLine(summary));
    return failed(summary) ? 1 : 0;
  } catch (error) {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    return 1;
  } finally {
    started?.stop();
  }
}

process.exit(await main());
