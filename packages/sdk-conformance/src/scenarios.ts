/** The scenarios in `fixtures/sdk/*.json`: loading them, running one, and the checks every one gets. */
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { list, record, show, str } from "./json.ts";
import { statusShape } from "./checks.ts";
import { bodyText, usageEvents } from "./requests.ts";
import { FakeServer, type Recorded } from "./server.ts";
import { REPO_ROOT, type Service } from "./service.ts";
import { type RunState, sentinelFor } from "./state.ts";
import { boolFlag, runStep } from "./steps.ts";

const SCENARIOS_DIR = join(REPO_ROOT, "fixtures/sdk");

export interface Scenario {
  file: string;
  name: string;
  contract: string;
  requires: string[];
  steps: unknown[];
}

export interface Outcome {
  scenario: Scenario;
  result: "pass" | "fail" | "skip";
  /** Why it failed or was skipped. */
  detail: string;
}

/**
 * Headers an HTTP library sends on its own, plus the protocol's. Anything else could identify a user or a
 * machine (section 12), so it fails the scenario.
 */
const STANDARD_HEADERS = new Set([
  "accept",
  "accept-encoding",
  "accept-language",
  "authorization",
  "cache-control",
  "connection",
  "content-length",
  "content-type",
  "expect",
  "host",
  "if-none-match",
  "keep-alive",
  "pragma",
  "te",
  "transfer-encoding",
  "user-agent",
]);

export function loadScenarios(dir = SCENARIOS_DIR): Scenario[] {
  return readdirSync(dir)
    .filter((file) => file.endsWith(".json"))
    .toSorted()
    .flatMap((file) => {
      const parsed = record(JSON.parse(readFileSync(join(dir, file), "utf8")), file);
      return list(parsed["scenarios"], `${file} scenarios`).map((raw) => {
        const entry = record(raw, `a scenario in ${file}`);
        return {
          file,
          name: str(entry, "name"),
          contract: str(entry, "contract"),
          requires: list(entry["requires"] ?? [], "requires").map(String),
          steps: list(entry["steps"], "steps"),
        };
      });
    });
}

/** What every request must be, whatever the scenario tested. */
function protocolProblem(request: Recorded, state: RunState): string | null {
  const where = `${request.method} ${request.path}`;
  if (request.route === "other") return `${where} is not one of the three SDK routes`;
  if (request.headers["authorization"] !== `Bearer ${state.server.key}`)
    return `${where} did not carry the key as a bearer token`;
  const extra = Object.keys(request.headers).find((name) => !STANDARD_HEADERS.has(name));
  if (extra !== undefined) return `${where} carried a header the SDK added: ${extra}`;
  if (request.route !== "events") return null;
  if (!(request.headers["content-type"] ?? "").startsWith("application/json"))
    return `${where} was not sent as application/json`;
  try {
    usageEvents(request);
  } catch (error) {
    return `${where}: ${String(error)}`;
  }
  return null;
}

function leaks(text: string, state: RunState): boolean {
  return text.includes(state.sentinel.key) || text.includes(state.sentinel.attribute);
}

/** Section 12: nothing about the context in any request, in a status or in an error the SDK built. */
async function privacyProblem(state: RunState): Promise<string | null> {
  for (const request of state.server.requests) {
    const parts = [request.path, ...Object.values(request.headers), bodyText(request)];
    if (parts.some((part) => leaks(part, state)))
      return `${request.method} ${request.path} carried the context's key or attribute`;
    const problem = protocolProblem(request, state);
    if (problem !== null) return problem;
  }
  if (state.client === null) return null;
  const errors = await state.service.client(state.client, "errors", "GET");
  if (leaks(JSON.stringify(errors["messages"] ?? []), state))
    return "an onError message carried the context";
  return statusShape(await state.service.client(state.client, "status", "GET"), state);
}

function recorded(server: FakeServer): string {
  const lines = server.requests.map((request) => {
    const body = bodyText(request);
    const answered = request.answered === null ? "no answer" : String(request.answered);
    return `    ${Math.round(request.at)} ms  ${request.method} ${request.path} -> ${answered}${body === "" ? "" : `  ${show(body, 200)}`}`;
  });
  return lines.length === 0 ? "    (nothing)" : lines.join("\n");
}

function failure(scenario: Scenario, done: number, why: string, server: FakeServer): string {
  const steps = scenario.steps.slice(0, done + 1).map((step, index) => {
    const pointer = index === done ? ">>" : "  ";
    return `  ${pointer} ${index + 1}. ${show(step, 400)}`;
  });
  return [why, "  steps:", ...steps, "  the server recorded:", recorded(server)].join("\n");
}

async function finish(state: RunState): Promise<void> {
  if (state.client !== null && !state.closed) {
    state.closed = true;
    await state.service.client(state.client, "close", "POST").catch(() => undefined);
  }
  await state.server.stop();
}

export async function runScenario(
  scenario: Scenario,
  index: number,
  service: Service,
  capabilities: string[],
): Promise<Outcome> {
  const missing = scenario.requires.filter((name) => !capabilities.includes(name));
  if (missing.length > 0)
    return { scenario, result: "skip", detail: `needs ${missing.join(", ")}` };
  const server = new FakeServer(`fs_server_conformance_${index}`);
  server.setRuleset([boolFlag("checkout", true), boolFlag("legacy", false)], []);
  server.start();
  const state: RunState = {
    server,
    service,
    client: null,
    closed: false,
    marks: new Map(),
    sentinel: sentinelFor(index),
  };
  let done = 0;
  try {
    for (const [at, step] of scenario.steps.entries()) {
      done = at;
      await runStep(state, step);
    }
    done = scenario.steps.length;
    await finish(state);
    const problem = await privacyProblem(state);
    if (problem !== null) throw new Error(problem);
    return { scenario, result: "pass", detail: "" };
  } catch (error) {
    const why = error instanceof Error ? error.message : String(error);
    await finish(state).catch(() => undefined);
    return {
      scenario,
      result: "fail",
      detail: failure(scenario, Math.min(done, scenario.steps.length - 1), why, server),
    };
  }
}
