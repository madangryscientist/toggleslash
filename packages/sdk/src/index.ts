/**
 * The ToggleSlash server-side SDK: it keeps a copy of the environment's ruleset current and evaluates
 * flags locally with `@hackcove/toggleslash-engine`. See `docs/sdk.md`.
 */
export {
  type Client,
  type ClientOptions,
  createClient,
  type DetailReason,
  type EvaluationEvent,
  type Hooks,
  type SdkErrorCode,
  type VariationDetail,
} from "./client.ts";
export type { Clock } from "./clock.ts";
export type { Fetch } from "./http.ts";
export type { ClientStatus, StreamState, SyncErrorCode } from "./sync.ts";
export type { Context } from "@hackcove/toggleslash-engine";
