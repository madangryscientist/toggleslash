import {
  type Context,
  type ErrorCode,
  evaluate,
  type Reason,
  type Ruleset,
} from "@hackcove/featureslash-engine";
import { type Clock, systemClock } from "./clock.ts";
import type { Fetch } from "./http.ts";
import { type ClientStatus, RulesetSync } from "./sync.ts";
import { UsageReporter } from "./usage.ts";

/**
 * What one evaluation did, handed to `hooks.afterEvaluation`. It carries the context the caller passed, so
 * a hook that sends it anywhere is the caller's decision (the SDK itself sends none of it).
 */
export interface EvaluationEvent {
  flagKey: string;
  context: Context;
  /** What the caller got: the served value, or its fallback. */
  value: unknown;
  /** The variation served, or null when `value` is the fallback or the flag serves nothing. */
  variationIndex: number | null;
  reason: DetailReason;
  /**
   * The ETag of the ruleset held at this evaluation, quoted as the server sent it (the same string as
   * `status().etag`), or null before the first load.
   */
  etag: string | null;
  /** The flag's `version` in that ruleset, or null when the flag is not in it or is malformed. */
  flagVersion: number | null;
}

export interface Hooks {
  /**
   * Called synchronously after every `variation` and `variationDetail`, a fallback included, with what the
   * caller is about to get. Its return value is ignored. An error it throws (or a promise it rejects) goes
   * to `onError` and never reaches the caller, and the caller gets the same answer with or without it.
   */
  afterEvaluation?: (event: EvaluationEvent) => void | PromiseLike<void>;
}

export interface ClientOptions {
  /** The server's base URL, e.g. `https://flags.example.com`. */
  url: string;
  /** A server SDK key (`fs_server_...`). A client key cannot read the ruleset. */
  sdkKey: string;
  /** For tests, or a runtime whose global `fetch` is not the one to use. */
  fetch?: Fetch;
  /** For tests: the time and the timers every wait goes through. */
  clock?: Clock;
  /** For tests: the source of the backoff jitter, in [0, 1). */
  random?: () => number;
  /** Follow the stream (default true). False polls only. */
  stream?: boolean;
  /** How often to poll when the stream is down or off, and the longest reconnect wait (default 30 s). */
  pollIntervalMs?: number;
  /** How often usage counts are sent (default 30 s). */
  flushIntervalMs?: number;
  /** The longest any one request may take, and so the longest `ready()` and `close()` wait (default 10 s). */
  requestTimeoutMs?: number;
  /** Optional observers of each evaluation; see {@link Hooks}. The SDK has no OpenTelemetry dependency. */
  hooks?: Hooks;
  /** Told about every failure the SDK absorbs. It is never needed for correctness; an error it throws is ignored. */
  onError?: (error: unknown) => void;
}

/** Why the caller got its fallback, on top of the engine's own errors. */
export type SdkErrorCode = ErrorCode | "NOT_READY" | "TYPE_MISMATCH" | "EVALUATION_FAILED";

export type DetailReason =
  | Exclude<Reason, { kind: "ERROR" }>
  | { kind: "ERROR"; code: SdkErrorCode };

export interface VariationDetail<T> {
  /** The served value, or the fallback when nothing usable was served. */
  value: T;
  /** The variation served, or null when `value` is the fallback or the flag serves nothing. */
  variationIndex: number | null;
  reason: DetailReason;
}

export interface Client {
  /**
   * Resolves once the first ruleset load has finished, with true if it worked. It resolves, never
   * rejects, and is bounded by the request timeout: false means flags serve fallbacks until the
   * SDK reaches the server.
   */
  ready(): Promise<boolean>;
  /** The flag's value for a context, or `fallback`. Synchronous, local, and it never throws. */
  variation<T>(flagKey: string, context: Context, fallback: T): T;
  /** Like `variation`, with the variation index and the reason. */
  variationDetail<T>(flagKey: string, context: Context, fallback: T): VariationDetail<T>;
  /**
   * What this SDK has loaded and how its stream is doing. Synchronous, reads memory, makes no network call
   * and never throws.
   */
  status(): ClientStatus;
  /** Stops following the server and sends the pending usage counts. Never rejects; safe to call twice. */
  close(): Promise<void>;
}

const DEFAULT_INTERVAL_MS = 30_000;
const DEFAULT_TIMEOUT_MS = 10_000;

/** Same JSON type: what a caller's `T` can mean at runtime. */
function sameKind<T>(value: unknown, fallback: T): value is T {
  if (fallback === null) return true; // a null fallback says nothing about the type the caller wants
  if (value === null) return false;
  return typeof value === typeof fallback && Array.isArray(value) === Array.isArray(fallback);
}

function readHook(hooks: Hooks | undefined): Hooks["afterEvaluation"] {
  const hook = hooks?.afterEvaluation;
  return typeof hook === "function" ? (event) => hook.call(hooks, event) : undefined;
}

function isThenable(value: unknown): value is PromiseLike<unknown> {
  return typeof value === "object" && value !== null && "then" in value;
}

/** The flag's version, when the ruleset has the flag and it validated far enough to carry one. */
function versionOf(ruleset: Ruleset | null, flagKey: string): number | null {
  const entry = ruleset?.flags.get(flagKey);
  return entry?.status === "ready" ? entry.flag.version : null;
}

function fallbackDetail<T>(fallback: T, code: SdkErrorCode): VariationDetail<T> {
  return { value: fallback, variationIndex: null, reason: { kind: "ERROR", code } };
}

class FeatureSlashClient implements Client {
  private readonly sync: RulesetSync;
  private readonly usage: UsageReporter;
  private readonly clock: Clock;
  private readonly onError: (error: unknown) => void;
  private readonly afterEvaluation: Hooks["afterEvaluation"];
  private readonly firstLoad = Promise.withResolvers<boolean>();
  private readonly flushLoop = new AbortController();
  private closing: Promise<void> | null = null;
  private loop: Promise<void>;
  private flushing: Promise<void>;

  constructor(options: ClientOptions) {
    this.clock = options.clock ?? systemClock;
    const report = options.onError;
    this.onError = (error) => {
      try {
        report?.(error);
      } catch {
        // A faulty error hook must not break the SDK.
      }
    };
    this.afterEvaluation = readHook(options.hooks);
    const baseUrl = typeof options.url === "string" ? options.url.replace(/\/+$/, "") : "";
    const shared = {
      baseUrl,
      sdkKey: options.sdkKey,
      fetch: options.fetch ?? ((input, init) => fetch(input, init)),
      clock: this.clock,
      requestTimeoutMs: options.requestTimeoutMs ?? DEFAULT_TIMEOUT_MS,
      onError: this.onError,
    };
    this.sync = new RulesetSync({
      ...shared,
      random: options.random ?? Math.random,
      stream: options.stream ?? true,
      pollIntervalMs: options.pollIntervalMs ?? DEFAULT_INTERVAL_MS,
    });
    this.usage = new UsageReporter(shared);
    this.loop = this.sync
      .run(() => this.firstLoad.resolve(this.sync.current !== null))
      .catch(this.onError);
    this.flushing = this.runFlushLoop(options.flushIntervalMs ?? DEFAULT_INTERVAL_MS);
  }

  ready(): Promise<boolean> {
    return this.firstLoad.promise;
  }

  variation<T>(flagKey: string, context: Context, fallback: T): T {
    return this.variationDetail(flagKey, context, fallback).value;
  }

  variationDetail<T>(flagKey: string, context: Context, fallback: T): VariationDetail<T> {
    const ruleset = this.sync.current;
    let detail: VariationDetail<T>;
    try {
      detail = this.detail(ruleset, flagKey, context, fallback);
    } catch (error) {
      this.onError(error);
      detail = fallbackDetail(fallback, "EVALUATION_FAILED");
    }
    if (this.afterEvaluation !== undefined) {
      const etag = this.sync.status.etag;
      this.runHook({ flagKey, context, etag, flagVersion: versionOf(ruleset, flagKey) }, detail);
    }
    return detail;
  }

  private runHook(
    event: Pick<EvaluationEvent, "flagKey" | "context" | "etag" | "flagVersion">,
    detail: VariationDetail<unknown>,
  ): void {
    try {
      const { value, variationIndex, reason } = detail;
      const result: unknown = this.afterEvaluation?.({ ...event, value, variationIndex, reason });
      if (isThenable(result)) Promise.resolve(result).catch(this.onError);
    } catch (error) {
      this.onError(error);
    }
  }

  status(): ClientStatus {
    return this.sync.status;
  }

  close(): Promise<void> {
    this.closing ??= this.shutDown();
    return this.closing;
  }

  private detail<T>(
    ruleset: Ruleset | null,
    flagKey: string,
    context: Context,
    fallback: T,
  ): VariationDetail<T> {
    if (ruleset === null) return fallbackDetail(fallback, "NOT_READY");
    const result = evaluate(ruleset, flagKey, context);
    const { value, variationIndex, reason } = result;
    if (variationIndex === null) return { value: fallback, variationIndex, reason };
    if (!sameKind(value, fallback)) return fallbackDetail(fallback, "TYPE_MISMATCH");
    if (!this.closing) this.usage.record(flagKey, variationIndex);
    return { value, variationIndex, reason };
  }

  private async runFlushLoop(intervalMs: number): Promise<void> {
    while (!this.flushLoop.signal.aborted) {
      await this.clock.sleep(intervalMs, this.flushLoop.signal);
      if (!this.flushLoop.signal.aborted) await this.usage.flush();
    }
  }

  private async shutDown(): Promise<void> {
    this.sync.stop();
    this.flushLoop.abort();
    await Promise.all([this.loop, this.flushing]);
    this.firstLoad.resolve(false);
    await this.usage.finish();
  }
}

export function createClient(options: ClientOptions): Client {
  return new FeatureSlashClient(options);
}
