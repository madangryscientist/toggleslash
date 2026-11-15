import { tmpdir } from "node:os";
import { expect, type Page } from "@playwright/test";
import { join } from "node:path";
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
/** The run's own temporary directory (`test-tmp-scope.ts` makes it), so the session goes with the run. */
export const signedInState = (): string =>
  join(process.env["TMPDIR"] ?? tmpdir(), "signed-in.json");

/** What `setup` handed over; throws when the stack was not started. */
export function stackInfo(): StackInfo {
  const raw = process.env[STACK_ENV];
  if (raw === undefined) throw new Error("the stack was not started (stack/setup.ts)");
  return JSON.parse(raw);
}
export const SKIP_ENV = "TOGGLESLASH_STACK_SKIP";

/**
 * Resolves once the dashboard's root has set `data-hydrated` on `<html>`, which it does only after React
 * hydrated: a click before it could land on a button with no handler yet. The root element has no role or
 * name to find it by, so this reads the attribute in the page.
 */
export async function hydrated(page: Page): Promise<void> {
  await expect
    .poll(() => page.evaluate<boolean>("document.documentElement.hasAttribute('data-hydrated')"))
    .toBe(true);
}

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
