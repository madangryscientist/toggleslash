import { join } from "node:path";
import { dotnetPath, REPO_ROOT } from "./dotnet.ts";
import { drain } from "./proc.ts";

const CSPROJ = join(
  REPO_ROOT,
  "apps",
  "server",
  "FeatureSlash.Server",
  "FeatureSlash.Server.csproj",
);
const LISTENING = /Now listening on: (http:\/\/127\.0\.0\.1:\d+)/;

export interface RunningServer {
  /** `http://127.0.0.1:<port>`, the port the OS picked. */
  url: string;
  /** Resolves with the exit code when the process ends, whether `stop()` ended it or not. */
  exited: Promise<number>;
  /** What it has written to stdout and stderr so far. */
  log(): string;
  /** Stops the process and waits for it to be gone. Safe to call twice. */
  stop(): Promise<void>;
}

async function dotnet(args: string[]): Promise<string> {
  const child = Bun.spawn([dotnetPath() ?? "dotnet", ...args], {
    cwd: REPO_ROOT,
    stdout: "pipe",
    stderr: "pipe",
  });
  const [out, err, code] = await Promise.all([
    child.stdout.text(),
    child.stderr.text(),
    child.exited,
  ]);
  if (code !== 0) throw new Error(`dotnet ${args.join(" ")} failed (${code}):\n${out}\n${err}`);
  return out;
}

/** Builds the server (incremental) and returns the path of its assembly. */
export async function buildServer(): Promise<string> {
  await dotnet(["build", CSPROJ, "--nologo", "-v", "quiet"]);
  // Evaluates the project only: msbuild prints just the property's value.
  const out = await dotnet(["msbuild", CSPROJ, "-nologo", "-getProperty:TargetPath"]);
  const path = out.trim();
  if (!path.endsWith("FeatureSlash.Server.dll")) {
    throw new Error(`dotnet did not report the assembly path: ${out}`);
  }
  return path;
}

/**
 * Starts the built server on a port the OS picks (`:0`, read back from its log, so there is no
 * race for a free one), on loopback only, with its SQLite file and admin token given here. The
 * token is a throwaway for this process; nothing about it is stored.
 */
export async function startServer(
  assembly: string,
  options: { dbPath: string; adminToken: string },
): Promise<RunningServer> {
  const child = Bun.spawn([dotnetPath() ?? "dotnet", assembly], {
    cwd: join(REPO_ROOT, "apps", "server", "FeatureSlash.Server"),
    env: {
      ...process.env,
      ASPNETCORE_URLS: "http://127.0.0.1:0",
      ASPNETCORE_ENVIRONMENT: "Production",
      FEATURESLASH_DB_PATH: options.dbPath,
      FEATURESLASH_ADMIN_TOKEN: options.adminToken,
    },
    stdin: "ignore",
    stdout: "pipe",
    stderr: "pipe",
  });

  let log = "";
  const listening = Promise.withResolvers<string>();
  const onText = (text: string) => {
    log += text;
    const match = LISTENING.exec(log);
    if (match?.[1] !== undefined) listening.resolve(match[1]);
  };
  const readers = [drain(child.stdout, onText), drain(child.stderr, onText)];
  void child.exited.then((code) =>
    listening.reject(new Error(`the server exited (${code}) before it listened:\n${log}`)),
  );

  let stopped: Promise<void> | null = null;
  const stop = (): Promise<void> => {
    stopped ??= (async () => {
      child.kill("SIGTERM");
      const killer = setTimeout(() => child.kill("SIGKILL"), 10_000);
      await child.exited;
      clearTimeout(killer);
      await Promise.all(readers);
    })();
    return stopped;
  };

  const timer = setTimeout(
    () => listening.reject(new Error(`the server did not listen in time:\n${log}`)),
    60_000,
  );
  try {
    const url = await listening.promise;
    return { url, exited: child.exited, log: () => log, stop };
  } catch (error) {
    await stop();
    throw error;
  } finally {
    clearTimeout(timer);
  }
}
