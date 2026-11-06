import { skipReason } from "../src/dotnet.ts";
import { startStack } from "../src/stack.ts";

/** What the spec needs to know about the stack, handed over in the environment (workers inherit it). */
export interface StackInfo {
  serverUrl: string;
  dashboardUrl: string;
  exampleUrl: string;
  adminToken: string;
  operatorPassword: string;
}

export const STACK_ENV = "TOGGLESLASH_STACK";
export const SKIP_ENV = "TOGGLESLASH_STACK_SKIP";

/** The stack's logs, for `logs-reporter.ts` to print when the run fails. */
declare global {
  var toggleslashStackLogs: (() => string) | undefined;
}

/**
 * Playwright `globalSetup`: starts the whole stack, and returns what stops it. Without a .NET SDK it
 * skips with a printed line, except in CI, where a missing SDK fails (`skipReason`).
 */
export default async function setup(): Promise<() => Promise<void>> {
  const skip = skipReason();
  if (skip !== null) {
    process.env[SKIP_ENV] = skip;
    return async () => undefined;
  }
  const stack = await startStack();
  const info: StackInfo = {
    serverUrl: stack.serverUrl,
    dashboardUrl: stack.dashboardUrl,
    exampleUrl: stack.exampleUrl,
    adminToken: stack.adminToken,
    operatorPassword: stack.operatorPassword,
  };
  process.env[STACK_ENV] = JSON.stringify(info);
  globalThis.toggleslashStackLogs = () => stack.logs();
  return () => stack.stop();
}
