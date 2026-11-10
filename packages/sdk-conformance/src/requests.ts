/** The `requests` check: what the harness's server received, filtered, counted and timed. */
import { inRange, list, optNum, optStr, type Rec, record, same, show, str } from "./json.ts";
import type { Recorded } from "./server.ts";
import { fill, mark, type RunState } from "./state.ts";

const decoder = new TextDecoder();

export function bodyText(request: Recorded): string {
  return decoder.decode(request.body);
}

function matching(state: RunState, check: Rec): { list: Recorded[]; since: number } {
  const route = optStr(check, "route") ?? "any";
  const since = check["since"] === undefined ? null : mark(state, str(check, "since"));
  const all = state.server.requests.slice(since?.seq ?? 0);
  return {
    list: all.filter((request) => route === "any" || request.route === route),
    since: since?.at ?? 0,
  };
}

/** Checks one `requests` entry and says what is wrong, or null. */
export function checkRequests(state: RunState, raw: unknown): string | null {
  const check = record(fill(raw, state), "a requests check");
  const { list: found, since } = matching(state, check);
  const label = `${optStr(check, "route") ?? "any"} requests`;
  const problems = [
    check["count"] === undefined ? null : inRange(found.length, check["count"], label),
    checkEach(found, check),
    checkTiming(found, check, since),
    check["events"] === undefined ? null : checkEvents(pick(found, check), check["events"]),
    check["batchSizes"] === undefined
      ? null
      : checkBatches(pick(found, check), check["batchSizes"]),
  ];
  return problems.find((problem) => problem !== null) ?? null;
}

function pick(found: Recorded[], check: Rec): Recorded[] {
  const nth = optNum(check, "nth");
  if (nth === undefined) return found;
  const one = found.at(nth);
  return one === undefined ? [] : [one];
}

function checkEach(found: Recorded[], check: Rec): string | null {
  const chosen = pick(found, check);
  if (check["nth"] !== undefined && chosen.length === 0)
    return `there is no request number ${show(check["nth"])}; ${found.length} matched`;
  for (const request of chosen) {
    const problem = checkOne(request, check);
    if (problem !== null)
      return `${request.method} ${request.path} at ${Math.round(request.at)} ms: ${problem}`;
  }
  return null;
}

function checkOne(request: Recorded, check: Rec): string | null {
  for (const field of ["answered", "method", "path"] as const) {
    const want = check[field];
    if (want !== undefined && request[field] !== want)
      return `${field} was ${show(request[field])}, expected ${show(want)}`;
  }
  const headers = check["headers"] === undefined ? {} : record(check["headers"], "headers");
  for (const [name, want] of Object.entries(headers)) {
    const got = request.headers[name.toLowerCase()] ?? null;
    if (got !== want) return `header ${name} was ${show(got)}, expected ${show(want)}`;
  }
  return null;
}

function gapsOf(found: Recorded[]): number[] {
  return found.slice(1).map((request, index) => request.at - (found[index]?.at ?? 0));
}

function checkTiming(found: Recorded[], check: Rec, since: number): string | null {
  const first = found[0];
  if (check["after"] !== undefined) {
    if (first === undefined) return "no request came, so none came in the window";
    const problem = inRange(
      Math.round(first.at - since),
      check["after"],
      "the first request came after (ms)",
    );
    if (problem !== null) return problem;
  }
  const gaps = gapsOf(found);
  const wanted = check["gaps"] === undefined ? [] : list(check["gaps"], "gaps");
  for (const [index, range] of wanted.entries()) {
    const gap = gaps[index];
    if (gap === undefined) return `there is no gap ${index}: ${found.length} requests matched`;
    const problem = inRange(Math.round(gap), range, `gap ${index} (ms)`);
    if (problem !== null) return `${problem}; the gaps were ${show(gaps.map(Math.round))}`;
  }
  const below = optNum(check, "someGapBelow");
  if (below !== undefined && !gaps.some((gap) => gap < below))
    return `no gap was below ${below} ms; the gaps were ${show(gaps.map(Math.round))}`;
  return null;
}

/** The usage events in one request body, read strictly: `{ "events": [ four fields each ] }` and nothing more. */
export function usageEvents(request: Recorded): Rec[] {
  const body: unknown = JSON.parse(bodyText(request));
  const top = record(body, "the events body");
  if (Object.keys(top).join() !== "events")
    throw new Error(`the events body has fields ${show(Object.keys(top))}`);
  return list(top["events"], "events").map((entry) => {
    const event = record(entry, "an event");
    if (Object.keys(event).toSorted().join() !== "count,day,flag,variationIndex")
      throw new Error(`an event has fields ${show(Object.keys(event))}`);
    return event;
  });
}

function summed(events: Rec[]): Map<string, number> {
  const totals = new Map<string, number>();
  for (const event of events) {
    const key = JSON.stringify([event["flag"], event["variationIndex"], event["day"]]);
    totals.set(key, (totals.get(key) ?? 0) + Number(event["count"]));
  }
  return totals;
}

function sortedTotals(totals: Map<string, number>): [string, number][] {
  return [...totals].toSorted(([a], [b]) => a.localeCompare(b));
}

function checkEvents(found: Recorded[], expected: unknown): string | null {
  const got = summed(found.flatMap(usageEvents));
  const want = summed(list(expected, "events").map((event) => record(event, "an event")));
  return same(sortedTotals(got), sortedTotals(want))
    ? null
    : `the usage sent, summed, was ${show(sortedTotals(got), 600)}, expected ${show(sortedTotals(want), 600)}`;
}

function checkBatches(found: Recorded[], expected: unknown): string | null {
  const sizes = found.map((request) => usageEvents(request).length);
  const want = list(expected, "batchSizes");
  if (sizes.length !== want.length)
    return `the batch sizes were ${show(sizes)}, expected ${show(want)}`;
  for (const [index, size] of sizes.entries()) {
    const problem = inRange(size, want[index], `batch ${index}`);
    if (problem !== null) return `${problem}; the batch sizes were ${show(sizes)}`;
  }
  return null;
}
