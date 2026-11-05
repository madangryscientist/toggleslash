import { randomBytes } from "node:crypto";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createAdmin } from "./admin.ts";
import { createCli } from "./cli.ts";
import { startDashboard } from "./dashboard.ts";
import { startExample } from "./example.ts";
import { seed } from "./seed.ts";
import { buildServer, startServer } from "./server.ts";

/** Fixed loopback ports, so a person (and a browser test) knows where to look. 4410 and 4420 are taken (FS-25). */
const STACK_PORTS = { dashboard: 4440, example: 4441 } as const;

interface Stack {
  serverUrl: string;
  dashboardUrl: string;
  exampleUrl: string;
  /** Throwaway, generated for this run. */
  adminToken: string;
  operatorPassword: string;
  /** Resolves with the log of whichever part exits while the stack is meant to be up. */
  failed: Promise<string>;
  /** Stops every process and removes the database. Safe to call twice; rejects if a part would not stop. */
  stop(): Promise<void>;
}

const secret = (bytes: number, encoding: "hex" | "base64url"): string =>
  randomBytes(bytes).toString(encoding);

/**
 * The whole product on loopback with the seeded shop (`seed.ts`): the real C# server on a fresh
 * SQLite file, the dashboard built and previewed against it, and the example app. Nothing is kept
 * between runs, and the credentials exist in this process and its children's environments only.
 * Aborting `signal` while it starts stops what has started and rejects.
 */
export async function startStack(signal?: AbortSignal): Promise<Stack> {
  const adminToken = secret(24, "hex");
  const operatorPassword = secret(18, "base64url");
  const sessionSecret = secret(32, "hex");
  const scratch = mkdtempSync(join(tmpdir(), "featureslash-stack-"));
  mkdirSync(join(scratch, "home"));

  const stoppers: (() => Promise<void>)[] = [];
  const failure = Promise.withResolvers<string>();
  let stopping: Promise<void> | null = null;
  const stop = (): Promise<void> => {
    stopping ??= (async () => {
      const failures: unknown[] = [];
      for (const stopOne of stoppers.toReversed()) await stopOne().catch((e) => failures.push(e));
      rmSync(scratch, { recursive: true, force: true });
      if (failures.length > 0)
        throw new AggregateError(failures, "a part of the stack did not stop");
    })();
    return stopping;
  };
  /** Says so when a part ends before `stop()` was called. */
  const watch = (name: string, exited: Promise<number>, log: () => string) => {
    void exited.then((code) => {
      if (stopping === null) failure.resolve(`${name} exited (${code}):\n${log()}`);
    });
  };

  try {
    const server = await startServer(await buildServer(), {
      dbPath: join(scratch, "featureslash.db"),
      adminToken,
    });
    stoppers.push(() => server.stop());
    watch("the server", server.exited, () => server.log());
    signal?.throwIfAborted();

    const cli = createCli({ url: server.url, token: adminToken, home: join(scratch, "home") });
    const keys = await seed({ cli, admin: createAdmin({ url: server.url, token: adminToken }) });
    signal?.throwIfAborted();

    const dashboard = await startDashboard({
      port: STACK_PORTS.dashboard,
      apiUrl: server.url,
      adminToken,
      operatorPassword,
      sessionSecret,
      tmpdir: join(scratch, "dashboard-tmp"),
      signal,
    });
    stoppers.push(() => dashboard.stop());
    signal?.throwIfAborted();
    watch(dashboard.name, dashboard.exited, () => dashboard.log());

    const example = await startExample({
      serverUrl: server.url,
      sdkKey: keys.serverKey,
      clientKey: keys.clientKey,
      port: STACK_PORTS.example,
      flushIntervalMs: 2_000, // usage counts show up in the dashboard within seconds
      tmpdir: scratch,
      signal,
    });
    stoppers.push(() => example.proc.stop());
    watch(example.proc.name, example.proc.exited, () => example.proc.log());

    return {
      serverUrl: server.url,
      dashboardUrl: `http://127.0.0.1:${STACK_PORTS.dashboard}`,
      exampleUrl: example.url,
      adminToken,
      operatorPassword,
      failed: failure.promise,
      stop,
    };
  } catch (error) {
    await stop().catch(() => undefined); // the start failure is the news
    throw error;
  }
}
