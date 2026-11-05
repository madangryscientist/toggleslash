import { type Clock, utcDay } from "./clock.ts";
import { type Fetch, retryAfterMs, withTimeout } from "./http.ts";

/** One line of `POST /sdk/v1/events`: counts only, never a context or anything about an end user. */
interface UsageEntry {
  flag: string;
  variationIndex: number;
  count: number;
  day: string;
}

/**
 * The server accepts 1000 events and 128 KiB a request; an entry is at most about 140 bytes, so
 * 500 stay under both.
 */
const MAX_BATCH = 500;

export interface UsageReporterOptions {
  baseUrl: string;
  sdkKey: string;
  fetch: Fetch;
  clock: Clock;
  requestTimeoutMs: number;
  onError: (error: unknown) => void;
}

/** What to do with a batch the server did not take: try it again later, or give up on it. */
function retryable(status: number): boolean {
  return status >= 500 || status === 408 || status === 429;
}

/**
 * Counts how often each flag variation was served, per UTC day, and sends the totals in batches.
 * `record` is a map increment, so it is safe on a request path. A batch that fails for a reason that
 * can pass (the server is down or busy) is put back; one the server refused is dropped, since it
 * would be refused again. After a 429 with a `Retry-After`, nothing is sent until that has passed.
 */
export class UsageReporter {
  private pending = new Map<string, UsageEntry>();
  private sending: Promise<void> | null = null;
  /** The server's 429 asked for nothing to reach it before this time. */
  private retryAt = 0;

  constructor(private readonly options: UsageReporterOptions) {}

  record(flag: string, variationIndex: number): void {
    this.add({
      flag,
      variationIndex,
      count: 1,
      day: utcDay(this.options.clock.now()),
    });
  }

  /** Sends what is pending. It never rejects; a call during a send waits for that send first. */
  flush(): Promise<void> {
    const run = (this.sending ?? Promise.resolve()).then(() => this.send());
    this.sending = run;
    return run;
  }

  /**
   * The last flush, on closing. It will not wait out a `Retry-After`, and it will not ignore one, so counts
   * still held back then are lost, and that is reported.
   */
  async finish(): Promise<void> {
    await this.flush();
    if (this.pending.size > 0 && this.holding()) {
      this.options.onError(
        new Error(`closed with ${this.pending.size} usage counts unsent: the server asked to wait`),
      );
    }
  }

  private holding(): boolean {
    return this.options.clock.now() < this.retryAt;
  }

  private add(entry: UsageEntry): void {
    const key = `${entry.flag}\n${entry.variationIndex}\n${entry.day}`;
    const existing = this.pending.get(key);
    if (existing) existing.count += entry.count;
    else this.pending.set(key, { ...entry });
  }

  private async send(): Promise<void> {
    if (this.holding()) return;
    const entries = [...this.pending.values()];
    this.pending = new Map();
    for (let start = 0; start < entries.length; start += MAX_BATCH) {
      const batch = entries.slice(start, start + MAX_BATCH);
      if (!(await this.post(batch))) {
        for (const entry of entries.slice(start)) this.add(entry);
        return;
      }
    }
  }

  /** False when the batch should be kept for another try. */
  private async post(batch: UsageEntry[]): Promise<boolean> {
    const { baseUrl, sdkKey, fetch, clock, requestTimeoutMs, onError } = this.options;
    try {
      const response = await withTimeout(clock, requestTimeoutMs, undefined, async (signal) => {
        const answer = await fetch(`${baseUrl}/sdk/v1/events`, {
          method: "POST",
          headers: { Authorization: `Bearer ${sdkKey}`, "Content-Type": "application/json" },
          body: JSON.stringify({ events: batch }),
          signal,
        });
        await answer.body?.cancel();
        return answer;
      });
      const { status } = response;
      if (status >= 200 && status < 300) return true;
      const wait = retryAfterMs(response);
      if (wait !== null) this.retryAt = Math.max(this.retryAt, clock.now() + wait);
      onError(new Error(`POST /sdk/v1/events answered ${status}`));
      return !retryable(status);
    } catch (error) {
      onError(error);
      return false;
    }
  }
}
