import type { Clock } from "../src/index.ts";

/** Lets every pending promise run: the fake server answers in microtasks, so a few turns settle it. */
export async function settle(): Promise<void> {
  for (let turn = 0; turn < 20; turn++) await new Promise<void>((resolve) => setImmediate(resolve));
}

interface Sleeper {
  at: number;
  wake: () => void;
}

/** A clock that moves only when a test advances it. */
export class FakeClock implements Clock {
  time = Date.UTC(2026, 10, 5, 12, 0, 0);
  private sleepers: Sleeper[] = [];

  now(): number {
    return this.time;
  }

  sleep(ms: number, signal: AbortSignal): Promise<void> {
    return new Promise<void>((resolve) => {
      if (signal.aborted) {
        resolve();
        return;
      }
      const sleeper: Sleeper = { at: this.time + ms, wake: done };
      function done(): void {
        resolve();
      }
      this.sleepers.push(sleeper);
      signal.addEventListener(
        "abort",
        () => {
          this.sleepers = this.sleepers.filter((other) => other !== sleeper);
          resolve();
        },
        { once: true },
      );
    });
  }

  /** Moves time forward, waking each sleeper in order and letting what it starts finish first. */
  async advance(ms: number): Promise<void> {
    const target = this.time + ms;
    await settle();
    for (;;) {
      const due = this.sleepers.filter((sleeper) => sleeper.at <= target);
      const next = due.toSorted((a, b) => a.at - b.at)[0];
      if (!next) break;
      this.sleepers = this.sleepers.filter((sleeper) => sleeper !== next);
      this.time = Math.max(this.time, next.at);
      next.wake();
      await settle();
    }
    this.time = target;
    await settle();
  }
}
