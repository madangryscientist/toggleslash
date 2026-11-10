/** The scenario steps, one function each, keyed by the step's `do`. */
import { runChecks } from "./checks.ts";
import {
  isRecord,
  list,
  num,
  optNum,
  optStr,
  partialMismatch,
  type Rec,
  record,
  show,
  str,
} from "./json.ts";
import type { Answer, Route } from "./server.ts";
import { errorCount, fill, type RunState, withSentinel } from "./state.ts";

type Step = (state: RunState, step: Rec) => Promise<void> | void;

const encoder = new TextEncoder();
const ROUTES: Route[] = ["ruleset", "stream", "events"];
/** How often `waitUntil` looks again. */
const POLL_MS = 25;

/** The client options a scenario gets unless it says otherwise. */
const CLIENT_DEFAULTS: Rec = {
  url: "{{url}}",
  sdkKey: "{{key}}",
  stream: true,
  pollIntervalMs: 30_000,
  flushIntervalMs: 30_000,
  requestTimeoutMs: 2000,
};

export function boolFlag(key: string, serves: boolean): Rec {
  return {
    key,
    variations: [false, true],
    on: true,
    offVariation: 0,
    targets: [],
    prerequisites: [],
    rules: [],
    fallthrough: { variation: serves ? 1 : 0 },
    salt: "s",
    version: 1,
  };
}

/** `{ "key": "f", "serves": true }` is a boolean flag; with `count`, that many, `{{i}}` numbering them. */
function expandFlag(raw: unknown): Rec[] {
  const entry = record(raw, "a flag");
  if (typeof entry["serves"] !== "boolean") return [entry];
  const serves = entry["serves"];
  const key = str(entry, "key");
  const count = optNum(entry, "count");
  if (count === undefined) return [boolFlag(key, serves)];
  return Array.from({ length: count }, (_, i) =>
    boolFlag(key.replaceAll("{{i}}", String(i)), serves),
  );
}

function route(step: Rec): Route {
  const name = str(step, "route");
  const found = ROUTES.find((known) => known === name);
  if (found === undefined) throw new Error(`no route "${name}"`);
  return found;
}

function chunk(raw: unknown): Uint8Array {
  if (typeof raw === "string") return encoder.encode(raw);
  return Uint8Array.from(list(raw, "a chunk").map(Number));
}

function answer(raw: unknown, state: RunState): Answer {
  const entry = record(raw, "an answer");
  const json = entry["json"];
  const body =
    json === undefined ? (optStr(entry, "body") ?? null) : JSON.stringify(fill(json, state));
  const headers =
    entry["headers"] === undefined ? {} : record(fill(entry["headers"], state), "headers");
  return {
    status: optNum(entry, "status") ?? 200,
    body,
    headers: Object.fromEntries(
      Object.entries(headers).map(([name, value]) => [name, String(value)]),
    ),
    hang: entry["hang"] === true,
    hangBody: entry["hangBody"] === true,
    sse: entry["sse"] === undefined ? null : list(fill(entry["sse"], state), "sse").map(chunk),
    end: entry["end"] === true,
    times: entry["times"] === "always" ? Infinity : (optNum(entry, "times") ?? 1),
  };
}

async function waitUntil(state: RunState, step: Rec): Promise<void> {
  const deadline = performance.now() + num(step, "withinMs");
  for (;;) {
    const problem = await runChecks(state, step);
    if (problem === null) return;
    if (performance.now() > deadline)
      throw new Error(`not so within ${num(step, "withinMs")} ms: ${problem}`);
    await Bun.sleep(POLL_MS);
  }
}

async function timed<T>(step: Rec, what: string, run: () => Promise<T>): Promise<T> {
  const started = performance.now();
  const result = await run();
  const took = Math.round(performance.now() - started);
  const within = optNum(step, "withinMs");
  if (within !== undefined && took > within)
    throw new Error(`${what} took ${took} ms, expected at most ${within}`);
  return result;
}

function client(state: RunState): string {
  if (state.client === null) throw new Error("no client was created");
  return state.client;
}

async function create(state: RunState, step: Rec): Promise<void> {
  if (state.client !== null) throw new Error("a scenario has one client");
  const given = step["options"] === undefined ? {} : record(step["options"], "options");
  state.client = await state.service.createClient(
    record(fill({ ...CLIENT_DEFAULTS, ...given }, state), "options"),
  );
}

async function ready(state: RunState, step: Rec): Promise<void> {
  const answered = await timed(step, "ready", () =>
    state.service.client(client(state), "ready", "POST"),
  );
  if (step["expect"] !== undefined && answered["ready"] !== step["expect"])
    throw new Error(`ready answered ${show(answered["ready"])}, expected ${show(step["expect"])}`);
}

/** `times` repeats the call; `each: n` makes n calls with `{{i}}` in the flag key counting from 0. */
function flagKeys(step: Rec): string[] {
  const flag = str(step, "flag");
  const each = optNum(step, "each");
  if (each !== undefined)
    return Array.from({ length: each }, (_, i) => flag.replaceAll("{{i}}", String(i)));
  return Array.from({ length: optNum(step, "times") ?? 1 }, () => flag);
}

async function evaluate(state: RunState, step: Rec): Promise<void> {
  if (!("fallback" in step)) throw new Error("an evaluate step names its fallback");
  const context = step["context"] === undefined ? "{{context}}" : step["context"];
  const expected =
    step["expect"] === undefined ? null : record(fill(step["expect"], state), "expect");
  for (const flagKey of flagKeys(step)) {
    const body = {
      flagKey,
      context: withSentinel(fill(context, state), state.sentinel),
      fallback: step["fallback"],
      accessor: optStr(step, "accessor") ?? "any",
    };
    const result = await timed(step, "evaluate", () =>
      state.service.client(client(state), "evaluate", "POST", body),
    );
    const problem = expected === null ? null : partialMismatch(result, expected);
    if (problem !== null)
      throw new Error(`evaluate ${flagKey}: ${problem} (answer: ${show(result)})`);
  }
}

async function close(state: RunState, step: Rec): Promise<void> {
  const calls = optNum(step, "calls") ?? 1;
  state.closed = true;
  await timed(step, "close", () =>
    Promise.all(
      Array.from({ length: calls }, () => state.service.client(client(state), "close", "POST")),
    ),
  );
}

async function expectStep(state: RunState, step: Rec): Promise<void> {
  const problem = await runChecks(state, step);
  if (problem !== null) throw new Error(problem);
}

async function markStep(state: RunState, step: Rec): Promise<void> {
  const errors = await errorCount(state);
  state.marks.set(str(step, "name"), {
    seq: state.server.requests.length,
    at: state.server.now(),
    wall: Date.now(),
    errors,
  });
}

function setRuleset(state: RunState, step: Rec): void {
  const flags = list(step["flags"] ?? [], "flags").flatMap(expandFlag);
  state.server.setRuleset(fill(flags, state), fill(step["segments"] ?? [], state));
}

export const STEPS: Record<string, Step> = {
  ruleset: setRuleset,
  script: (state, step) =>
    state.server.script(
      route(step),
      list(step["answers"], "answers").map((raw) => answer(raw, state)),
    ),
  create,
  ready,
  evaluate,
  close,
  wait: (_state, step) => Bun.sleep(num(step, "ms")),
  waitUntil,
  expect: expectStep,
  mark: markStep,
  publish: (state) => state.server.publish(),
  write: (state, step) =>
    state.server.writeChunks(list(fill(step["chunks"], state), "chunks").map(chunk)),
  dropStreams: (state) => state.server.dropStreams(),
  down: (state) => state.server.stop(),
  up: (state) => state.server.start(),
};

export async function runStep(state: RunState, raw: unknown): Promise<void> {
  if (!isRecord(raw)) throw new Error(`a step is not an object: ${show(raw)}`);
  const name = str(raw, "do");
  const run = Object.hasOwn(STEPS, name) ? STEPS[name] : undefined;
  if (run === undefined) throw new Error(`no step "${str(raw, "do")}"`);
  await run(state, raw);
}
