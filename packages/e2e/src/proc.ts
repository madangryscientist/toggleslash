const LOG_LIMIT = 64 * 1024;

export interface ManagedProcess {
  name: string;
  /** What it wrote to stdout and stderr, the last 64 KiB of it. */
  log(): string;
  /** Resolves with the exit code when it ends, whether `stop()` ended it or not. */
  exited: Promise<number>;
  /** Ends it and everything it started, and waits. Safe to call twice. */
  stop(): Promise<void>;
}

export async function drain(stream: ReadableStream<Uint8Array>, into: (text: string) => void) {
  const decoder = new TextDecoder();
  for await (const chunk of stream) into(decoder.decode(chunk, { stream: true }));
}

/**
 * A long-running child with its output kept. It leads its own process group, so `stop()` also ends
 * what it started (`vite preview` starts a `workerd`); SIGKILL follows after ten seconds. `signal`
 * aborting stops it.
 */
export function spawnManaged(
  name: string,
  command: string[],
  options: { cwd: string; env: Record<string, string>; signal?: AbortSignal | undefined },
): ManagedProcess {
  const child = Bun.spawn(command, {
    cwd: options.cwd,
    env: options.env,
    detached: true,
    stdin: "ignore",
    stdout: "pipe",
    stderr: "pipe",
  });
  let log = "";
  const keep = (text: string) => {
    log = (log + text).slice(-LOG_LIMIT);
  };
  const readers = Promise.all([drain(child.stdout, keep), drain(child.stderr, keep)]);

  const signal = (kind: "SIGTERM" | "SIGKILL") => {
    try {
      process.kill(-child.pid, kind);
    } catch {
      // The group is already gone.
    }
  };
  let stopped: Promise<void> | null = null;
  const stop = (): Promise<void> => {
    stopped ??= (async () => {
      signal("SIGTERM");
      const killer = setTimeout(() => signal("SIGKILL"), 10_000);
      await child.exited;
      clearTimeout(killer);
      signal("SIGKILL"); // whatever outlived its parent
      await readers;
    })();
    return stopped;
  };
  // An aborted start ends it, so a Ctrl-C while the stack is still starting leaves nothing behind.
  if (options.signal?.aborted) void stop();
  else options.signal?.addEventListener("abort", () => void stop(), { once: true });
  return { name, log: () => log, exited: child.exited, stop };
}

/** Polls `check` until it is true; fails with the process's log if it exits first, or after `timeoutMs`. */
export async function waitReady(
  proc: ManagedProcess,
  what: string,
  check: () => boolean | Promise<boolean>,
  timeoutMs = 60_000,
): Promise<void> {
  const ended: { code: number | null } = { code: null };
  void proc.exited.then((code) => {
    ended.code = code;
  });
  const deadline = Date.now() + timeoutMs;
  while (!(await check())) {
    if (ended.code !== null) {
      throw new Error(`${proc.name} exited (${ended.code}) before ${what}:\n${proc.log()}`);
    }
    if (Date.now() > deadline) {
      throw new Error(`${proc.name} was not ready (${what}) in ${timeoutMs} ms:\n${proc.log()}`);
    }
    await Bun.sleep(50);
  }
}

/** Runs a command to its end; its output is part of the error when it fails. */
export async function runStep(
  name: string,
  command: string[],
  options: { cwd: string; env: Record<string, string> },
): Promise<void> {
  const child = Bun.spawn(command, {
    cwd: options.cwd,
    env: options.env,
    stdin: "ignore",
    stdout: "pipe",
    stderr: "pipe",
  });
  const [out, err, code] = await Promise.all([
    child.stdout.text(),
    child.stderr.text(),
    child.exited,
  ]);
  if (code !== 0) {
    throw new Error(`${name} failed (${code}):\n${(out + err).slice(-LOG_LIMIT)}`);
  }
}

/** The few variables a child needs, so a secret in the caller's environment is not handed to it. */
export function baseEnv(extra: Record<string, string>): Record<string, string> {
  const keep = [
    "PATH",
    "HOME",
    "TMPDIR",
    "LANG",
    "XDG_CACHE_HOME",
    "XDG_CONFIG_HOME",
    "XDG_DATA_HOME",
  ];
  const env: Record<string, string> = {};
  for (const name of keep) {
    const value = process.env[name];
    if (value !== undefined) env[name] = value;
  }
  return { ...env, ...extra };
}
