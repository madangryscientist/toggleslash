/** Time as the SDK sees it, injectable so a test moves it by hand instead of waiting. */
export interface Clock {
  /** Milliseconds since the Unix epoch. */
  now(): number;
  /** Resolves after `ms`, or as soon as `signal` aborts. It never rejects. */
  sleep(ms: number, signal: AbortSignal): Promise<void>;
}

/** The real clock. Its timers are unref'd, so a forgotten client never keeps a process alive. */
export const systemClock: Clock = {
  now: () => Date.now(),
  sleep: (ms, signal) =>
    new Promise<void>((resolve) => {
      if (signal.aborted) {
        resolve();
        return;
      }
      const done = (): void => {
        clearTimeout(timer);
        signal.removeEventListener("abort", done);
        resolve();
      };
      const timer = setTimeout(done, ms);
      timer.unref?.();
      signal.addEventListener("abort", done, { once: true });
    }),
};

/** Runs `onFire` after `ms` unless the returned function is called first. */
export function startTimer(clock: Clock, ms: number, onFire: () => void): () => void {
  const cancel = new AbortController();
  void clock.sleep(ms, cancel.signal).then(() => {
    if (!cancel.signal.aborted) onFire();
  });
  return () => {
    cancel.abort();
  };
}

/** The UTC day of a time, `yyyy-MM-dd`, as `POST /sdk/v1/events` wants it. */
export function utcDay(epochMs: number): string {
  return new Date(epochMs).toISOString().slice(0, 10);
}
