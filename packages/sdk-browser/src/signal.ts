import {
  type Clock,
  type Fetch,
  linkedController,
  retryAfterMs,
  SseParser,
} from "@hackcove/toggleslash-sdk/shared";
import { Watchdog } from "./support.ts";

const MIN_BACKOFF_MS = 1_000;
const MAX_BACKOFF_MS = 30_000;
/** The server sends a comment every 25 s; a stream silent for this long is dead. */
const STREAM_IDLE_MS = 60_000;

export interface ChangeSignalOptions {
  baseUrl: string;
  clientKey: string;
  fetch: Fetch;
  clock: Clock;
  requestTimeoutMs: number;
  /** Aborts when the client closes or its key is refused. */
  lifetime: AbortSignal;
  /** The server said `connected` or `changed`: evaluate again. */
  onSignal: () => void;
  onError: (error: unknown) => void;
}

/**
 * `GET /sdk/v1/changes` with the key in a header, read as a stream with `fetch` (`EventSource` cannot
 * send a header). It reconnects after 1 s, doubling to 30 s, and the wait resets on `connected`.
 */
export class ChangeSignal {
  /** True from a `connected` event until the stream ends. */
  up = false;
  private backoffMs = MIN_BACKOFF_MS;

  constructor(private readonly options: ChangeSignalOptions) {}

  async run(): Promise<void> {
    const { lifetime, clock } = this.options;
    while (!lifetime.aborted) {
      const retryAfter = await this.open();
      this.up = false;
      if (lifetime.aborted) return;
      const wait = retryAfter ?? this.backoffMs;
      this.backoffMs = Math.min(this.backoffMs * 2, MAX_BACKOFF_MS);
      await clock.sleep(wait, lifetime);
    }
  }

  /** One connection, until it ends or fails. Returns what a 429 asked to wait, or null. */
  private async open(): Promise<number | null> {
    const { baseUrl, clientKey, fetch, clock, requestTimeoutMs, lifetime, onError } = this.options;
    const { controller, dispose } = linkedController(lifetime);
    const watchdog = new Watchdog(clock, controller);
    watchdog.arm(requestTimeoutMs, "the change signal did not open");
    let retryAfter: number | null = null;
    try {
      const response = await fetch(`${baseUrl}/sdk/v1/changes`, {
        headers: { Authorization: `Bearer ${clientKey}`, Accept: "text/event-stream" },
        signal: controller.signal,
      });
      if (response.status !== 200 || response.body === null) {
        retryAfter = retryAfterMs(response);
        await response.body?.cancel();
        throw new Error(`GET /sdk/v1/changes answered ${response.status}`);
      }
      await this.pump(response.body.getReader(), watchdog);
    } catch (error) {
      if (!lifetime.aborted) onError(error);
    } finally {
      watchdog.disarm();
      controller.abort();
      dispose();
    }
    return retryAfter;
  }

  private async pump(
    reader: { read: () => Promise<{ done: boolean; value?: Uint8Array | undefined }> },
    watchdog: Watchdog,
  ): Promise<void> {
    const decoder = new TextDecoder();
    const parser = new SseParser();
    for (;;) {
      watchdog.arm(STREAM_IDLE_MS, "the change signal went quiet");
      const { done, value } = await reader.read();
      if (done) return;
      for (const event of parser.feed(decoder.decode(value, { stream: true }))) {
        if (event.event === "connected") {
          this.up = true;
          this.backoffMs = MIN_BACKOFF_MS;
        } else if (event.event !== "changed") continue;
        this.options.onSignal();
      }
    }
  }
}
