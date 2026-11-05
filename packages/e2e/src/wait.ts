const TIMEOUT_MS = 10_000;

/** Polls `check` every 25 ms until it returns true, or fails with `what` after 10 s. */
export async function until(what: string, check: () => boolean | Promise<boolean>): Promise<void> {
  const deadline = Date.now() + TIMEOUT_MS;
  while (!(await check())) {
    if (Date.now() > deadline)
      throw new Error(`timed out after ${TIMEOUT_MS} ms waiting for ${what}`);
    await Bun.sleep(25);
  }
}
