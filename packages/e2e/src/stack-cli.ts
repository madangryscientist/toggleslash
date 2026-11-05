import { hasDotnet } from "./dotnet.ts";
import { startStack } from "./stack.ts";

/** `bun run stack`: the whole product on loopback with a seeded shop, until Ctrl-C. */
const out = (line: string) => process.stdout.write(`${line}\n`);

if (!hasDotnet()) {
  process.stderr.write(
    [
      "stack: no .NET SDK matching global.json runs here, and the stack needs the real server.",
      "Run `mise install`, then start it with the mise SDK first on PATH:",
      '  PATH="$(dirname "$(mise which dotnet)"):$PATH" bun run stack',
      "",
    ].join("\n"),
  );
  process.exit(1);
}

// Ctrl-C during the start aborts it, and the start stops whatever it had begun.
const starting = new AbortController();
const abortStart = () => starting.abort();
process.on("SIGINT", abortStart);
process.on("SIGTERM", abortStart);

out("Starting the stack: building the server and the dashboard takes a minute the first time.");
const stack = await startStack(starting.signal).catch((error: unknown) => {
  if (starting.signal.aborted) return process.exit(130);
  process.stderr.write(
    `stack: could not start:\n${error instanceof Error ? error.message : String(error)}\n`,
  );
  return process.exit(1);
});
process.off("SIGINT", abortStart);
process.off("SIGTERM", abortStart);

out("");
out("Everything below is a throwaway for this run. Nothing is kept when you stop it.");
out("");
out(`  Example app   ${stack.exampleUrl}/?user=pro-user`);
out(`  Dashboard     ${stack.dashboardUrl}   (sign in with the password below)`);
out(`  Server        ${stack.serverUrl}`);
out("");
out(`  Operator password   ${stack.operatorPassword}`);
out(`  Admin token         ${stack.adminToken}`);
out("");
out("  Use the CLI against it (project shop, environment dev):");
out(`    export FEATURESLASH_URL=${stack.serverUrl}`);
out(`    export FEATURESLASH_TOKEN=${stack.adminToken}`);
out("    bun run featureslash flags on legacy-cart --project shop --env dev");
out("");
out("Press Ctrl-C to stop.");

let ending = false;
async function finish(code: number): Promise<never> {
  if (!ending) {
    ending = true;
    try {
      await stack.stop();
    } catch (error) {
      process.stderr.write(`stack: ${String(error)}\nSomething may still be running.\n`);
      return process.exit(1);
    }
  }
  return process.exit(code);
}
process.on("SIGINT", () => void finish(0));
process.on("SIGTERM", () => void finish(0));
const log = await stack.failed;
process.stderr.write(`\nstack: ${log}\nStopping the rest.\n`);
await finish(1);
