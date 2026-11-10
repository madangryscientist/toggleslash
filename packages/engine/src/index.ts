/**
 * The ToggleSlash engine: flag evaluation for TypeScript, implementing `docs/evaluation.md`. It uses
 * nothing runtime-specific, so it runs in Workers, Bun, Node and browsers.
 */
export type { Context, SingleContext } from "./context.ts";
export { evaluate } from "./evaluate.ts";
export type { ErrorCode, EvaluationResult, Reason } from "./result.ts";
export { createRuleset, type Ruleset } from "./ruleset.ts";

export const ENGINE_NAME = "toggleslash-engine";

export const ENGINE_VERSION = "0.0.0";

/** What the engine is, in one line, for `--version` and logs. */
export function describeEngine(): string {
  return `${ENGINE_NAME} ${ENGINE_VERSION}`;
}
