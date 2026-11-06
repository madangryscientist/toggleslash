import { join } from "node:path";
import { REPO_ROOT } from "./dotnet.ts";
import { baseEnv, spawnManaged, waitReady, type ManagedProcess } from "./proc.ts";

export interface RunningExample {
  /** `http://127.0.0.1:<port>`. */
  url: string;
  proc: ManagedProcess;
}

/**
 * The example app (`apps/example`) as a subprocess, configured as a deployment would be: by the
 * environment. `port` 0 lets the OS pick; the app says which it got.
 */
export async function startExample(options: {
  serverUrl: string;
  sdkKey: string;
  clientKey: string;
  port: number;
  flushIntervalMs?: number;
  tmpdir: string;
  signal?: AbortSignal | undefined;
}): Promise<RunningExample> {
  const proc = spawnManaged("the example app", ["bun", "src/server.ts"], {
    cwd: join(REPO_ROOT, "apps", "example"),
    signal: options.signal,
    env: baseEnv({
      TMPDIR: options.tmpdir,
      PORT: String(options.port),
      TOGGLESLASH_URL: options.serverUrl,
      TOGGLESLASH_SDK_KEY: options.sdkKey,
      TOGGLESLASH_CLIENT_KEY: options.clientKey,
      ...(options.flushIntervalMs === undefined
        ? {}
        : { TOGGLESLASH_FLUSH_INTERVAL_MS: String(options.flushIntervalMs) }),
    }),
  });
  let url = "";
  try {
    await waitReady(proc, "it listened", () => {
      url = /example: listening on (http:\/\/127\.0\.0\.1:\d+)/.exec(proc.log())?.[1] ?? "";
      return url !== "";
    });
  } catch (error) {
    await proc.stop();
    throw error;
  }
  return { url, proc };
}
