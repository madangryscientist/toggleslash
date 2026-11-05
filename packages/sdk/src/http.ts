import { type Clock, startTimer } from "./clock.ts";

/** `fetch`, narrowed to what the SDK calls, so a test can pass a fake. */
export type Fetch = (input: string, init?: RequestInit) => Promise<Response>;

/** A request or a stream that did not answer in time. */
export class TimeoutError extends Error {}

/** However long a server asks, a client is back within this. */
const MAX_RETRY_AFTER_MS = 300_000;

/**
 * How long a 429 asks to wait, capped. Null for any other answer, and for a `Retry-After` that is missing
 * or is not whole seconds (the server never sends a date), which leaves the caller's usual backoff in charge.
 */
export function retryAfterMs(response: Response): number | null {
  if (response.status !== 429) return null;
  const header = response.headers.get("Retry-After")?.trim() ?? "";
  if (!/^\d+$/.test(header)) return null;
  return Math.min(Number(header) * 1000, MAX_RETRY_AFTER_MS);
}

/** An `AbortController` that also aborts when `parent` does. Call `dispose` when done with it. */
export function linkedController(parent: AbortSignal | undefined): {
  controller: AbortController;
  dispose: () => void;
} {
  const controller = new AbortController();
  const onParent = (): void => {
    controller.abort(parent?.reason);
  };
  if (parent?.aborted) onParent();
  else parent?.addEventListener("abort", onParent, { once: true });
  return { controller, dispose: () => parent?.removeEventListener("abort", onParent) };
}

/**
 * Runs `run` with a signal that aborts after `ms` or when `parent` does. Covers the whole of `run`,
 * so reading a body counts toward the limit and a server that stalls cannot hold a loop forever.
 */
export async function withTimeout<T>(
  clock: Clock,
  ms: number,
  parent: AbortSignal | undefined,
  run: (signal: AbortSignal) => Promise<T>,
): Promise<T> {
  const { controller, dispose } = linkedController(parent);
  const cancel = startTimer(clock, ms, () => {
    controller.abort(new TimeoutError(`no answer within ${ms} ms`));
  });
  try {
    return await run(controller.signal);
  } finally {
    cancel();
    dispose();
  }
}
