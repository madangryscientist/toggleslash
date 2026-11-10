/** The checks an `expect` or `waitUntil` step makes, each answering what is wrong, or null. */
import {
  inRange,
  isRecord,
  list,
  optStr,
  partialMismatch,
  type Rec,
  record,
  show,
  str,
} from "./json.ts";
import { checkRequests } from "./requests.ts";
import { errorCount, fill, mark, type RunState } from "./state.ts";

const STREAM_STATES = ["off", "connecting", "connected", "disconnected", "closed"];
const ERROR_CODE = /^(http_\d{3}|timeout|bad_ruleset|network)$/;
const CHECKS = ["requests", "status", "errors", "hookEvents", "openStreams"];

function checkNames(step: Rec): string[] {
  return Object.keys(step).filter((name) => CHECKS.includes(name));
}

function client(state: RunState): string {
  if (state.client === null) throw new Error("no client was created");
  return state.client;
}

/** The four fields, each of its kind, and nothing else (section 8). */
export function statusShape(status: Rec, state: RunState): string | null {
  if (Object.keys(status).toSorted().join() !== "etag,lastError,lastSyncAt,stream")
    return `status has fields ${show(Object.keys(status))}`;
  const text = JSON.stringify(status);
  if (text.includes(state.server.key) || text.includes(state.sentinel.key))
    return "status holds the SDK key or a context key";
  return fieldKinds(status);
}

function fieldKinds(status: Rec): string | null {
  const { etag, lastSyncAt, stream, lastError } = status;
  if (etag !== null && typeof etag !== "string") return `status etag is ${show(etag)}`;
  if (lastSyncAt !== null && !Number.isInteger(lastSyncAt))
    return `status lastSyncAt is ${show(lastSyncAt)}, not whole epoch ms or null`;
  if (typeof stream !== "string" || !STREAM_STATES.includes(stream))
    return `status stream is ${show(stream)}`;
  if (lastError !== null && !(typeof lastError === "string" && ERROR_CODE.test(lastError)))
    return `status lastError is ${show(lastError)}`;
  return null;
}

function checkSyncAt(actual: unknown, want: unknown, state: RunState): string | null {
  if (want === null)
    return actual === null ? null : `lastSyncAt was ${show(actual)}, expected null`;
  if (typeof actual !== "number") return `lastSyncAt was ${show(actual)}, expected a time`;
  if (want === "set") return null;
  const range = record(want, "lastSyncAt");
  const after = optStr(range, "after");
  const before = optStr(range, "before");
  if (after !== undefined && actual < mark(state, after).wall)
    return `lastSyncAt ${actual} is before the mark "${after}"`;
  if (before !== undefined && actual > mark(state, before).wall)
    return `lastSyncAt ${actual} is after the mark "${before}"`;
  return null;
}

async function checkStatus(state: RunState, raw: unknown): Promise<string | null> {
  const status = await state.service.client(client(state), "status", "GET");
  const shape = statusShape(status, state);
  if (shape !== null) return shape;
  const { lastSyncAt, ...rest } = record(fill(raw, state), "status");
  const mismatch = partialMismatch(status, rest);
  if (mismatch !== null) return `status ${mismatch} (status: ${show(status)})`;
  return lastSyncAt === undefined ? null : checkSyncAt(status["lastSyncAt"], lastSyncAt, state);
}

async function checkErrors(state: RunState, raw: unknown): Promise<string | null> {
  const count = await errorCount(state);
  if (!isRecord(raw) || raw["since"] === undefined) return inRange(count, raw, "onError calls");
  const range = Object.fromEntries(Object.entries(raw).filter(([name]) => name !== "since"));
  const since = str(raw, "since");
  return inRange(count - mark(state, since).errors, range, `onError calls since "${since}"`);
}

async function checkHookEvents(state: RunState, raw: unknown): Promise<string | null> {
  const answer = await state.service.client(client(state), "hook-events", "GET");
  const events = list(answer["events"], "hook events");
  const want = record(fill(raw, state), "hookEvents");
  if (want["count"] !== undefined) {
    const problem = inRange(events.length, want["count"], "hook events");
    if (problem !== null) return problem;
  }
  const expected = want["events"] === undefined ? [] : list(want["events"], "hookEvents.events");
  for (const [index, partial] of expected.entries()) {
    const event = events[index];
    if (!isRecord(event)) return `there is no hook event ${index}; there were ${events.length}`;
    const mismatch = partialMismatch(event, record(partial, "a hook event"));
    if (mismatch !== null) return `hook event ${index}: ${mismatch}`;
  }
  return null;
}

/** Runs every check the step names. */
export async function runChecks(state: RunState, step: Rec): Promise<string | null> {
  for (const name of checkNames(step)) {
    const problem = await runCheck(state, name, step[name]);
    if (problem !== null) return problem;
  }
  return null;
}

function runCheck(
  state: RunState,
  name: string,
  raw: unknown,
): Promise<string | null> | string | null {
  if (name === "status") return checkStatus(state, raw);
  if (name === "errors") return checkErrors(state, raw);
  if (name === "hookEvents") return checkHookEvents(state, raw);
  if (name === "openStreams") return inRange(state.server.openStreams, raw, "open streams");
  const checks = Array.isArray(raw) ? raw : [raw];
  for (const check of checks) {
    const problem = checkRequests(state, check);
    if (problem !== null) return problem;
  }
  return null;
}
