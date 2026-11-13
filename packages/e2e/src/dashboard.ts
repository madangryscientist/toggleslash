import { mkdirSync, rmSync } from "node:fs";
import { join } from "node:path";
import { REPO_ROOT } from "./dotnet.ts";
import { baseEnv, runStep, spawnManaged, waitReady, type ManagedProcess } from "./proc.ts";

const WEB = join(REPO_ROOT, "apps", "web");

interface DashboardOptions {
  port: number;
  /** The C# server the dashboard talks to, and the admin token it sends. */
  apiUrl: string;
  adminToken: string;
  operatorPassword: string;
  sessionSecret: string;
  /** Where the build and the Worker keep their temporary files. */
  tmpdir: string;
  /** Aborting it stops the dashboard, even while it is still starting. */
  signal?: AbortSignal | undefined;
}

/**
 * Builds the dashboard and serves it with `vite preview`, as `apps/web/playwright.config.ts` does
 * for its own run, but against a real server instead of the fake API. `TOGGLESLASH_E2E` keeps the
 * local state apart from `vite dev`'s and turns off the debugger port that two runs would fight
 * over. The D1 database is an unused placeholder, so it is not migrated.
 */
export async function startDashboard(options: DashboardOptions): Promise<ManagedProcess> {
  mkdirSync(options.tmpdir, { recursive: true });
  const base = { TMPDIR: options.tmpdir, TOGGLESLASH_E2E: "1" };

  // The secrets are bound when the preview starts, not at build: the build copies its variables
  // to dist/server/.dev.vars, which would beat what the preview sets.
  const build = baseEnv({ ...base, CLOUDFLARE_INCLUDE_PROCESS_ENV: "false" });
  await runStep("the dashboard build", ["bun", "run", "build"], { cwd: WEB, env: build });
  rmSync(join(WEB, "dist", "server", ".dev.vars"), { force: true });

  const proc = spawnManaged(
    "the dashboard",
    [
      "bunx",
      "vite",
      "preview",
      "--host",
      "127.0.0.1",
      "--port",
      String(options.port),
      "--strictPort",
    ],
    {
      cwd: WEB,
      signal: options.signal,
      env: baseEnv({
        ...base,
        // Lets the Worker read these from the process environment, as a deployed one reads its secrets.
        CLOUDFLARE_INCLUDE_PROCESS_ENV: "true",
        TOGGLESLASH_API_URL: options.apiUrl,
        TOGGLESLASH_ADMIN_TOKEN: options.adminToken,
        TOGGLESLASH_OPERATOR_PASSWORD: options.operatorPassword,
        TOGGLESLASH_SESSION_SECRET: options.sessionSecret,
      }),
    },
  );
  try {
    await waitReady(proc, "it answered", async () => {
      try {
        // A preview that accepts the connection and never answers must not hold the loop forever:
        // the abort rejects into the catch below, which reads as "not ready yet", so the loop keeps
        // polling and fails with the log once `waitReady` passes its deadline (up to 5 s after it).
        const response = await fetch(`http://127.0.0.1:${options.port}/sign-in`, {
          signal: AbortSignal.timeout(5_000),
        });
        return response.ok;
      } catch {
        return false;
      }
    });
  } catch (error) {
    await proc.stop();
    throw error;
  }
  return proc;
}
