import type { FullResult, Reporter } from "@playwright/test/reporter";

/** When the run did not pass, prints what the stack's children wrote: the server, the dashboard, the app. */
export default class LogsReporter implements Reporter {
  onEnd(result: FullResult): void {
    const logs = globalThis.toggleslashStackLogs?.();
    if (result.status === "passed" || logs === undefined) return;
    process.stderr.write(`\nThe stack's logs:\n${logs}\n`);
  }
  printsToStdio(): boolean {
    return false;
  }
}
