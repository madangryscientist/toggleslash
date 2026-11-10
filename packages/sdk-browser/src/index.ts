/**
 * The ToggleSlash browser SDK: the server evaluates, this client caches the answer for one context and
 * follows the change signal. It has no ruleset and no evaluator. See `docs/sdk-browser.md`.
 */
export { type BrowserClient, type BrowserClientOptions, createBrowserClient } from "./client.ts";
export type {
  Context,
  ContextEntry,
  ErrorCode,
  MultiContext,
  Reason,
  SingleContext,
  VariationDetail,
} from "./types.ts";
export type { Clock, Fetch } from "@hackcove/toggleslash-sdk/shared";
