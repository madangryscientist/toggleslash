import {
  type Clock,
  type Fetch,
  systemClock,
  UsageReporter,
  withTimeout,
} from "@hackcove/toggleslash-sdk/shared";
import { changedKeys, readAnswer } from "./answer.ts";
import { ChangeSignal } from "./signal.ts";
import { fallbackDetail, sameKind } from "./support.ts";
import type { Context, Evaluation, VariationDetail } from "./types.ts";

export interface BrowserClientOptions {
  /** The server, as the server SDK's `url` option. */
  url: string;
  /** A client SDK key (`tgl_client_...`). It goes in an `Authorization` header, never in a URL. */
  clientKey: string;
  /** Who the flags are evaluated for: a single or a multi context, as `POST /sdk/v1/evaluate` takes it. */
  context: Context;
  /** Default: the global `fetch`. */
  fetch?: Fetch;
  /** Default: the system clock. */
  clock?: Clock;
  /** Values served before the first answer arrives. They are not evaluations and record no usage. */
  bootstrap?: Record<string, unknown>;
  /** How often to evaluate again while the change signal is down (default 60 s). */
  pollIntervalMs?: number;
  /** The longest any one request may take (default 10 s). */
  requestTimeoutMs?: number;
  /** Send usage counts (default true). */
  sendUsage?: boolean;
  /** Told about every failure the client absorbs. Default: nothing. An error it throws is ignored. */
  onError?: (error: unknown) => void;
}

export interface BrowserClient {
  /** Settles after the first evaluate answer or failure. It never rejects. */
  ready(): Promise<void>;
  /** The flag's value from the cache, or `fallback`. Synchronous, no network, never throws. */
  variation<T>(flag: string, fallback: T): T;
  /** Like `variation`, with the variation index and the reason. */
  detail<T>(flag: string, fallback: T): VariationDetail<T>;
  /** Every cached value by flag key (the bootstrap values before the first answer). */
  allFlags(): Record<string, unknown>;
  /** Evaluates for a new context. Until its answer arrives the previous context's values keep serving. Never rejects. */
  identify(context: Context): Promise<void>;
  /** Called with the sorted keys whose value or variation index changed. Returns the unsubscribe function. */
  on(event: "change", listener: (flags: string[]) => void): () => void;
  /** Stops the signal and the timers and sends the pending usage counts. Never rejects; safe to call twice. */
  close(): Promise<void>;
}

const DEFAULT_POLL_MS = 60_000;
const DEFAULT_TIMEOUT_MS = 10_000;
const FLUSH_INTERVAL_MS = 30_000;

/** One in-flight evaluation loop for one context. A signal that arrives meanwhile sets `again`. */
interface Run {
  generation: number;
  again: boolean;
  done: Promise<void>;
}

function defaultFetch(): Fetch {
  return typeof globalThis.fetch === "function"
    ? globalThis.fetch.bind(globalThis)
    : () => Promise.reject(new Error("no fetch is available"));
}

class Client implements BrowserClient {
  private readonly baseUrl: string;
  private readonly clientKey: string;
  private readonly fetch: Fetch;
  private readonly clock: Clock;
  private readonly requestTimeoutMs: number;
  private readonly onError: (error: unknown) => void;
  private readonly usage: UsageReporter | null;
  private readonly signal: ChangeSignal;
  private readonly bootstrap: Record<string, unknown>;
  /** Aborts when the client is closed or the key is refused. */
  private readonly lifetime = new AbortController();
  private readonly listeners = new Set<(flags: string[]) => void>();
  private readonly loops: Promise<void>[];
  private readonly firstSettled = Promise.withResolvers<void>();

  /** The last answer applied; null until one arrives. */
  private cache: ReadonlyMap<string, Evaluation> | null = null;
  /** The request body for the current context; null if the context could not be serialised. */
  private body: string | null = null;
  /** Bumped by `identify`: an answer for an older context is dropped. */
  private generation = 0;
  private issued = 0;
  private applied = 0;
  private running: Run | null = null;
  private halted = false;
  private closing: Promise<void> | null = null;

  constructor(options: BrowserClientOptions) {
    this.baseUrl = options.url.replace(/\/+$/, "");
    this.clientKey = options.clientKey;
    this.fetch = options.fetch ?? defaultFetch();
    this.clock = options.clock ?? systemClock;
    this.requestTimeoutMs = options.requestTimeoutMs ?? DEFAULT_TIMEOUT_MS;
    this.onError = options.onError ?? (() => undefined);
    this.bootstrap = { ...options.bootstrap };
    this.signal = new ChangeSignal({
      baseUrl: this.baseUrl,
      clientKey: this.clientKey,
      fetch: this.guarded,
      clock: this.clock,
      requestTimeoutMs: this.requestTimeoutMs,
      lifetime: this.lifetime.signal,
      onSignal: () => void this.refresh(),
      onError: (error) => {
        this.report(error);
      },
    });
    this.usage = options.sendUsage === false ? null : this.makeUsage();
    this.setContext(options.context);
    void this.refresh();
    this.loops = [this.signal.run(), this.poll(options.pollIntervalMs ?? DEFAULT_POLL_MS)];
    if (this.usage) this.loops.push(this.flushLoop(this.usage));
  }

  ready(): Promise<void> {
    return this.firstSettled.promise;
  }

  variation<T>(flag: string, fallback: T): T {
    return this.detail(flag, fallback).value;
  }

  detail<T>(flag: string, fallback: T): VariationDetail<T> {
    const cache = this.cache;
    if (cache === null) return this.bootstrapped(flag, fallback);
    const entry = cache.get(flag);
    if (entry === undefined) return fallbackDetail(fallback, "FLAG_NOT_FOUND");
    const { value, variationIndex, reason } = entry;
    if (variationIndex === null) return { value: fallback, variationIndex, reason };
    if (!sameKind(value, fallback)) return fallbackDetail(fallback, "TYPE_MISMATCH");
    if (this.usage && !this.lifetime.signal.aborted) this.usage.record(flag, variationIndex);
    return { value, variationIndex, reason };
  }

  allFlags(): Record<string, unknown> {
    if (this.cache === null) return { ...this.bootstrap };
    return Object.fromEntries([...this.cache].map(([key, entry]) => [key, entry.value]));
  }

  identify(context: Context): Promise<void> {
    if (this.lifetime.signal.aborted) return Promise.resolve();
    this.generation++;
    this.setContext(context);
    return this.refresh();
  }

  on(event: "change", listener: (flags: string[]) => void): () => void {
    if (event !== "change" || typeof listener !== "function") return () => undefined;
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  close(): Promise<void> {
    this.closing ??= this.shutDown();
    return this.closing;
  }

  /** Before the first answer: a bootstrap value, which no server answer stands behind. */
  private bootstrapped<T>(flag: string, fallback: T): VariationDetail<T> {
    if (!Object.hasOwn(this.bootstrap, flag)) return fallbackDetail(fallback, "NOT_READY");
    const value = this.bootstrap[flag];
    if (!sameKind(value, fallback)) return fallbackDetail(fallback, "TYPE_MISMATCH");
    return { value, variationIndex: null, reason: { kind: "ERROR", code: "NOT_READY" } };
  }

  private makeUsage(): UsageReporter {
    return new UsageReporter({
      baseUrl: this.baseUrl,
      sdkKey: this.clientKey,
      fetch: this.guarded,
      clock: this.clock,
      requestTimeoutMs: this.requestTimeoutMs,
      onError: (error) => {
        this.report(error);
      },
    });
  }

  private async shutDown(): Promise<void> {
    this.lifetime.abort();
    await Promise.all(this.loops);
    await this.running?.done;
    this.firstSettled.resolve();
    if (this.usage && !this.halted) await this.usage.finish();
  }

  private setContext(context: Context): void {
    try {
      this.body = JSON.stringify({ context });
    } catch (error) {
      this.body = null;
      this.report(error);
    }
  }

  private emit(error: unknown): void {
    try {
      this.onError(error);
    } catch {
      // the caller's handler is not ours to fail on
    }
  }

  /** Reports a failure, unless the key was refused: that is reported once, by `halt`. */
  private report(error: unknown): void {
    if (!this.halted) this.emit(error);
  }

  /** The key will not work: stop the signal, the polling and usage for good, and say so once. */
  private halt(what: string, status: number): void {
    if (this.halted) return;
    this.halted = true;
    this.lifetime.abort();
    this.emit(
      new Error(`${what} answered ${status}: the client key is refused, so the client stopped`),
    );
  }

  /** `fetch` that treats a 401 or 403 on any route as the end of this client. */
  private readonly guarded: Fetch = async (input, init) => {
    const response = await this.fetch(input, init);
    if (response.status === 401 || response.status === 403)
      this.halt(`${init?.method ?? "GET"} ${input.slice(this.baseUrl.length)}`, response.status);
    return response;
  };

  /**
   * Evaluates for the current context. A call during an evaluation for the same context asks for one more
   * when it ends, however many come; a new context starts its own at once.
   */
  private refresh(): Promise<void> {
    if (this.lifetime.signal.aborted) return Promise.resolve();
    const current = this.running;
    if (current !== null && current.generation === this.generation) {
      current.again = true;
      return current.done;
    }
    const run: Run = { generation: this.generation, again: false, done: Promise.resolve() };
    run.done = this.cycle(run);
    this.running = run;
    return run.done;
  }

  private async cycle(run: Run): Promise<void> {
    try {
      do {
        run.again = false;
        await this.evaluate(run.generation);
      } while (run.again && run.generation === this.generation && !this.lifetime.signal.aborted);
    } finally {
      if (this.running === run) this.running = null;
    }
  }

  private async evaluate(generation: number): Promise<void> {
    const body = this.body;
    const seq = ++this.issued;
    try {
      if (body === null) return;
      const text = await withTimeout(
        this.clock,
        this.requestTimeoutMs,
        this.lifetime.signal,
        async (signal) => {
          const response = await this.guarded(`${this.baseUrl}/sdk/v1/evaluate`, {
            method: "POST",
            headers: {
              Authorization: `Bearer ${this.clientKey}`,
              "Content-Type": "application/json",
            },
            body,
            signal,
          });
          if (!response.ok) {
            await response.body?.cancel();
            throw new Error(`POST /sdk/v1/evaluate answered ${response.status}`);
          }
          return await response.text();
        },
      );
      this.apply(readAnswer(text), seq, generation);
    } catch (error) {
      if (!this.lifetime.signal.aborted) this.report(error);
    } finally {
      this.firstSettled.resolve();
    }
  }

  /** Takes an answer unless a newer one, or a newer context, got there first. */
  private apply(answer: Map<string, Evaluation>, seq: number, generation: number): void {
    if (generation !== this.generation || seq <= this.applied) return;
    this.applied = seq;
    const changed = changedKeys(this.cache ?? new Map(), answer);
    this.cache = answer;
    if (changed.length === 0) return;
    for (const listener of this.listeners) {
      try {
        listener([...changed]);
      } catch (error) {
        this.report(error);
      }
    }
  }

  /** Evaluates every `pollIntervalMs` while the signal is down. */
  private async poll(intervalMs: number): Promise<void> {
    while (!this.lifetime.signal.aborted) {
      await this.clock.sleep(intervalMs, this.lifetime.signal);
      if (!this.lifetime.signal.aborted && !this.signal.up) void this.refresh();
    }
  }

  private async flushLoop(usage: UsageReporter): Promise<void> {
    while (!this.lifetime.signal.aborted) {
      await this.clock.sleep(FLUSH_INTERVAL_MS, this.lifetime.signal);
      if (!this.lifetime.signal.aborted) await usage.flush();
    }
  }
}

/**
 * Makes a browser client. The server evaluates: this client holds the answer for one context, serves it
 * synchronously, and asks again when the change signal says something changed. It never throws.
 */
export function createBrowserClient(options: BrowserClientOptions): BrowserClient {
  return new Client(options);
}
