import { createRuleset, type Ruleset } from "@hackcove/toggleslash-engine";
import { type Clock, startTimer } from "./clock.ts";
import { type Fetch, linkedController, retryAfterMs, TimeoutError, withTimeout } from "./http.ts";
import { SseParser } from "./sse.ts";

/** `off`: `stream: false`. `connecting`: opening it, or the first load. `connected`: the server's `connected` event arrived. `disconnected`: down, waiting to retry (polling meanwhile). `closed`: after `close()`. */
export type StreamState = "off" | "connecting" | "connected" | "disconnected" | "closed";
export type SyncErrorCode = `http_${number}` | "timeout" | "bad_ruleset" | "network";
export interface ClientStatus {
  /** The ETag of the ruleset held; null before the first load. */
  etag: string | null;
  /** When the SDK last confirmed its ruleset matches the server, as epoch milliseconds from the clock; null if never. */
  lastSyncAt: number | null;
  stream: StreamState;
  /** Why the most recent ruleset or stream attempt failed; null if it did not. */
  lastError: SyncErrorCode | null;
}

class HttpStatusError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

class BadRulesetError extends Error {}

function errorCode(error: unknown): SyncErrorCode {
  if (error instanceof HttpStatusError) return `http_${error.status}`;
  if (error instanceof TimeoutError) return "timeout";
  if (error instanceof BadRulesetError || error instanceof SyntaxError) return "bad_ruleset";
  return "network";
}

export interface SyncOptions {
  baseUrl: string;
  sdkKey: string;
  fetch: Fetch;
  clock: Clock;
  random: () => number;
  stream: boolean;
  pollIntervalMs: number;
  requestTimeoutMs: number;
  onError: (error: unknown) => void;
}

/** The server sends a keep-alive every 25 seconds; this long without a byte and the stream is dead. */
const STREAM_IDLE_MS = 60_000;
const BACKOFF_BASE_MS = 1_000;

function hasRulesetShape(body: unknown): boolean {
  if (typeof body !== "object" || body === null) return false;
  return (
    "flags" in body &&
    Array.isArray(body.flags) &&
    "segments" in body &&
    Array.isArray(body.segments)
  );
}

/**
 * Keeps one ruleset current: it fetches with `If-None-Match`, follows the stream when it can and polls
 * when it cannot. Nothing in here throws or rejects; every failure goes to `onError` and leaves the
 * last good ruleset in place.
 */
export class RulesetSync {
  private ruleset: Ruleset | null = null;
  private etag: string | null = null;
  /** The server's 429 asked for nothing to reach it before this time. */
  private retryAt = 0;
  private readonly lifetime = new AbortController();
  private lastSyncAt: number | null = null;
  private lastError: SyncErrorCode | null = null;
  private streamState: StreamState;

  constructor(private readonly options: SyncOptions) {
    this.streamState = options.stream ? "connecting" : "off";
  }

  get current(): Ruleset | null {
    return this.ruleset;
  }

  /** Local state only: what this SDK has loaded and how its stream is doing. The server never sees it. */
  get status(): ClientStatus {
    return {
      etag: this.etag,
      lastSyncAt: this.lastSyncAt,
      stream: this.lifetime.signal.aborted ? "closed" : this.streamState,
      lastError: this.lastError,
    };
  }

  /** The ruleset is confirmed current as of now. */
  private synced(): void {
    this.lastSyncAt = this.options.clock.now();
    this.lastError = null;
  }

  stop(): void {
    this.lifetime.abort();
  }

  /** Loads once, then follows until `stop`. Resolves when it has stopped. */
  async run(onFirstLoad: () => void): Promise<void> {
    await this.refresh();
    onFirstLoad();
    let failures = 0;
    while (!this.lifetime.signal.aborted) {
      // A 429 on the ruleset says wait, and without this the stream would be asked straight after it.
      if (this.options.stream) await this.sleep(0);
      if (this.lifetime.signal.aborted) return;
      const connected = this.options.stream ? await this.follow() : false;
      failures = connected ? 0 : failures + 1;
      await this.sleep(this.delay(failures));
      // Sleep first: the ruleset was only just loaded, or the stream only just failed. A stream that was
      // up needs no poll, since reconnecting announces the tag and catches up on its own.
      if (!connected && !this.lifetime.signal.aborted) await this.refresh();
    }
  }

  /** Sleeps for `ms`, or until the server's `Retry-After` has passed if that is later. */
  private async sleep(ms: number): Promise<void> {
    const { clock } = this.options;
    const wait = Math.max(ms, this.retryAt - clock.now());
    if (wait > 0) await clock.sleep(wait, this.lifetime.signal);
  }

  /** Remembers what a 429 asked for; a shorter answer never shortens a longer wait. */
  private honourRetryAfter(response: Response): void {
    const wait = retryAfterMs(response);
    if (wait !== null) this.retryAt = Math.max(this.retryAt, this.options.clock.now() + wait);
  }

  /** Exponential from a second up to the poll interval, jittered; with no stream, just the interval. */
  private delay(failures: number): number {
    const { pollIntervalMs, stream, random } = this.options;
    if (!stream) return pollIntervalMs;
    const ceiling = Math.min(pollIntervalMs, BACKOFF_BASE_MS * 2 ** Math.min(failures, 16));
    return ceiling * (0.5 + random() / 2);
  }

  /** One conditional fetch of the ruleset. */
  async refresh(): Promise<boolean> {
    const { baseUrl, sdkKey, fetch, clock, requestTimeoutMs, onError } = this.options;
    try {
      await withTimeout(clock, requestTimeoutMs, this.lifetime.signal, async (signal) => {
        const headers: Record<string, string> = { Authorization: `Bearer ${sdkKey}` };
        if (this.etag !== null && this.ruleset !== null) headers["If-None-Match"] = this.etag;
        const response = await fetch(`${baseUrl}/sdk/v1/ruleset`, { headers, signal });
        if (response.status === 304) {
          await response.body?.cancel();
          return;
        }
        if (response.status !== 200) {
          this.honourRetryAfter(response);
          await response.body?.cancel();
          throw new HttpStatusError(
            response.status,
            `GET /sdk/v1/ruleset answered ${response.status}`,
          );
        }
        const body: unknown = await response.json();
        if (!hasRulesetShape(body))
          throw new BadRulesetError("GET /sdk/v1/ruleset sent no flags and segments");
        this.ruleset = createRuleset(body);
        this.etag = response.headers.get("ETag");
      });
      this.synced();
      return true;
    } catch (error) {
      if (!this.lifetime.signal.aborted) {
        this.lastError = errorCode(error);
        onError(error);
      }
      return false;
    }
  }

  /**
   * Follows `GET /sdk/v1/stream` until it ends or fails. True if it connected, so the caller can tell a
   * stream that was up from one that never was.
   */
  private async follow(): Promise<boolean> {
    const { baseUrl, sdkKey, fetch, clock, requestTimeoutMs, onError } = this.options;
    const { controller, dispose } = linkedController(this.lifetime.signal);
    const watchdog = new Watchdog(clock, controller);
    watchdog.arm(requestTimeoutMs, "the stream did not open");
    const state = { connected: false };
    this.streamState = "connecting";
    try {
      const response = await fetch(`${baseUrl}/sdk/v1/stream`, {
        headers: { Authorization: `Bearer ${sdkKey}`, Accept: "text/event-stream" },
        signal: controller.signal,
      });
      if (response.status !== 200 || response.body === null) {
        this.honourRetryAfter(response);
        await response.body?.cancel();
        throw new HttpStatusError(
          response.status,
          `GET /sdk/v1/stream answered ${response.status}`,
        );
      }
      await this.pump(response.body.getReader(), watchdog, state);
    } catch (error) {
      if (!this.lifetime.signal.aborted) {
        this.lastError = errorCode(error);
        onError(error);
      }
    } finally {
      watchdog.disarm();
      controller.abort();
      dispose();
      this.streamState = "disconnected";
    }
    return state.connected;
  }

  /** Reads events until the stream ends; the watchdog drops one that goes quiet. */
  private async pump(
    reader: { read: () => Promise<{ done: boolean; value?: Uint8Array | undefined }> },
    watchdog: Watchdog,
    state: { connected: boolean },
  ): Promise<void> {
    const decoder = new TextDecoder();
    const parser = new SseParser();
    for (;;) {
      watchdog.arm(STREAM_IDLE_MS, "the stream went quiet");
      const { done, value } = await reader.read();
      if (done) return;
      for (const event of parser.feed(decoder.decode(value, { stream: true }))) {
        if (event.event !== "changed" && event.event !== "connected") continue;
        if (event.event === "connected") {
          state.connected = true;
          this.streamState = "connected";
          this.lastError = null;
        }
        if (!(await this.onTag(event.data))) {
          // The fetch the stream asked for failed, and a stream that stays up would never retry it.
          // End it and count it as not connected, so the backoff and the poll take over.
          state.connected = false;
          return;
        }
      }
    }
  }

  /** The stream names the ruleset's ETag; fetch only when it is not the one held. */
  private async onTag(tag: string): Promise<boolean> {
    if (this.ruleset !== null && tag === this.etag) {
      this.synced();
      return true;
    }
    return this.refresh();
  }
}

/** One timer at a time that aborts `controller` with a reason; arming it again replaces the last. */
class Watchdog {
  private cancel: () => void = () => undefined;

  constructor(
    private readonly clock: Clock,
    private readonly controller: AbortController,
  ) {}

  arm(ms: number, reason: string): void {
    this.cancel();
    this.cancel = startTimer(this.clock, ms, () => {
      this.controller.abort(new TimeoutError(reason));
    });
  }

  disarm(): void {
    this.cancel();
  }
}
