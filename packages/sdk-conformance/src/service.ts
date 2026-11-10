/**
 * The SDK's test service, as the harness sees it: started from a shell command or reached at a URL, and
 * driven over the protocol in `docs/sdk-test-service.md`.
 */
import { type ChildProcess, spawn } from "node:child_process";
import { join } from "node:path";
import { list, type Rec, record, show, str, toJson } from "./json.ts";

export const REPO_ROOT = join(import.meta.dirname, "../../..");

const LISTENING = /^listening (http:\/\/127\.0\.0\.1:\d+)\s*$/m;
const START_TIMEOUT_MS = 30_000;
/** Longer than any step waits on the service: a call that takes this long is a hung SDK. */
const CALL_TIMEOUT_MS = 20_000;

export interface Started {
  url: string;
  /** Everything the service wrote to stderr so far. */
  stderr: () => string;
  stop: () => void;
}

/** Runs `command` with `sh -c` from the repo root and waits for its `listening` line. */
export function startService(command: string): Promise<Started> {
  const child = spawn("sh", ["-c", command], {
    cwd: REPO_ROOT,
    detached: true,
    stdio: ["ignore", "pipe", "pipe"],
  });
  let stdout = "";
  let stderr = "";
  child.stderr?.on("data", (chunk: Buffer) => (stderr += chunk.toString()));
  const stop = (): void => kill(child);
  return new Promise((resolve, reject) => {
    const fail = (why: string): void => {
      stop();
      reject(new Error(`the test service ${why}.\nIts stderr:\n${stderr || "(empty)"}`));
    };
    const timer = setTimeout(
      () => fail(`printed no listening line within ${START_TIMEOUT_MS / 1000} s`),
      START_TIMEOUT_MS,
    );
    child.on("exit", (code) => {
      clearTimeout(timer);
      fail(`exited with code ${code} before it printed a listening line`);
    });
    child.stdout?.on("data", (chunk: Buffer) => {
      stdout += chunk.toString();
      const url = LISTENING.exec(stdout)?.[1];
      if (url === undefined) return;
      clearTimeout(timer);
      child.removeAllListeners("exit");
      resolve({ url, stderr: () => stderr, stop });
    });
  });
}

/** The service runs under `sh`, so the whole process group goes. */
function kill(child: ChildProcess): void {
  if (child.pid === undefined || child.exitCode !== null) return;
  try {
    process.kill(-child.pid, "SIGKILL");
  } catch {
    // Already gone.
  }
}

class ServiceError extends Error {}

export class Service {
  constructor(readonly url: string) {}

  async call(method: string, path: string, body?: unknown): Promise<Rec> {
    const init: RequestInit = { method, signal: AbortSignal.timeout(CALL_TIMEOUT_MS) };
    if (body !== undefined) {
      init.body = toJson(body);
      init.headers = { "Content-Type": "application/json" };
    }
    let response: Response;
    try {
      response = await fetch(new URL(path, this.url), init);
    } catch (error) {
      throw new ServiceError(`${method} ${path} to the service failed: ${String(error)}`);
    }
    const text = await response.text();
    if (response.status === 500)
      throw new ServiceError(`the SDK threw during ${method} ${path}:\n${text}`);
    if (!response.ok)
      throw new ServiceError(`${method} ${path} answered ${response.status}: ${show(text)}`);
    const parsed: unknown = text === "" ? {} : JSON.parse(text);
    return record(parsed, `the answer to ${method} ${path}`);
  }

  async describe(): Promise<{ name: string; capabilities: string[] }> {
    const answer = await this.call("GET", "/");
    const capabilities = list(answer["capabilities"], "capabilities").map(String);
    return { name: str(answer, "name"), capabilities };
  }

  async createClient(options: Rec): Promise<string> {
    const answer = await this.call("POST", "/clients", options);
    return str(answer, "id");
  }

  client(id: string, action: string, method: "GET" | "POST", body?: unknown): Promise<Rec> {
    return this.call(method, `/clients/${encodeURIComponent(id)}/${action}`, body);
  }
}
