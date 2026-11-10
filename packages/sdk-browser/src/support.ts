import type { Clock } from "@hackcove/toggleslash-sdk/shared";
import type { ErrorCode, VariationDetail } from "./types.ts";

/** Same JSON type: what a caller's `T` can mean at runtime. */
export function sameKind<T>(value: unknown, fallback: T): value is T {
  if (fallback === null) return true; // a null fallback says nothing about the type the caller wants
  if (value === null) return false;
  return typeof value === typeof fallback && Array.isArray(value) === Array.isArray(fallback);
}

export function fallbackDetail<T>(fallback: T, code: ErrorCode): VariationDetail<T> {
  return { value: fallback, variationIndex: null, reason: { kind: "ERROR", code } };
}

/** One timer at a time that aborts `controller`; arming it again replaces the last. */
export class Watchdog {
  private cancel: () => void = () => undefined;

  constructor(
    private readonly clock: Clock,
    private readonly controller: AbortController,
  ) {}

  arm(ms: number, reason: string): void {
    this.cancel();
    const timer = new AbortController();
    void this.clock.sleep(ms, timer.signal).then(() => {
      if (!timer.signal.aborted) this.controller.abort(new Error(reason));
    });
    this.cancel = () => {
      timer.abort();
    };
  }

  disarm(): void {
    this.cancel();
  }
}
