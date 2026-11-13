/**
 * Every evaluation vector in `fixtures/evaluation` (but `buckets.json`, which tests the bucket function
 * alone) through the service: the harness serves the vector's flags and segments, and the SDK must answer
 * what the vector expects.
 */
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { list, partialMismatch, type Rec, record, show, str, toJson } from "./json.ts";
import { FakeServer } from "./server.ts";
import { REPO_ROOT, type Service } from "./service.ts";

const DIR = join(REPO_ROOT, "fixtures/evaluation");

/**
 * Contract 5.5 makes a ruleset whose `flags` or `segments` is not an array `bad_ruleset`, so an SDK holds
 * no ruleset there and answers `NOT_READY`; the evaluator alone counts them as empty.
 */
const SKIPPED = new Set([
  "load: flags that is not an array counts as empty",
  "load: segments that is not an array counts as empty",
]);

const CONCURRENCY = 16;

interface Vector {
  name: string;
  flagKey: string;
  flags: unknown;
  segments: unknown;
  context: unknown;
  expected: Rec;
}

export interface VectorOutcome {
  passed: number;
  skipped: number;
  failures: string[];
}

function loadVectors(): Vector[] {
  return readdirSync(DIR)
    .filter((file) => file.endsWith(".json") && file !== "buckets.json")
    .toSorted()
    .flatMap((file) =>
      list(record(JSON.parse(readFileSync(join(DIR, file), "utf8")), file)["vectors"], file).map(
        (raw) => {
          const entry = record(raw, `a vector in ${file}`);
          return {
            name: str(entry, "name"),
            flagKey: str(entry, "flagKey"),
            flags: entry["flags"],
            segments: entry["segments"],
            context: entry["context"],
            expected: record(entry["expected"], "expected"),
          };
        },
      ),
    );
}

/** Vectors with the same flags and segments share one client. */
function grouped(vectors: Vector[]): Vector[][] {
  const groups = new Map<string, Vector[]>();
  for (const vector of vectors) {
    const key = toJson([vector.flags, vector.segments]);
    groups.set(key, [...(groups.get(key) ?? []), vector]);
  }
  return [...groups.values()];
}

async function runGroup(
  service: Service,
  server: FakeServer,
  sdkKey: string,
  group: Vector[],
): Promise<string[]> {
  const id = await service.createClient({
    url: server.url,
    sdkKey,
    stream: false,
    pollIntervalMs: 600_000,
    flushIntervalMs: 600_000,
    requestTimeoutMs: 5000,
  });
  const failures: string[] = [];
  try {
    await service.client(id, "ready", "POST");
    for (const vector of group) {
      const body = {
        flagKey: vector.flagKey,
        context: vector.context,
        fallback: null,
        accessor: "any",
      };
      const answer = await service.client(id, "evaluate", "POST", body);
      const actual = {
        value: answer["value"],
        variationIndex: answer["variationIndex"],
        reason: answer["reason"],
      };
      const problem = partialMismatch(actual, vector.expected);
      if (problem !== null) failures.push(`${vector.name}: ${problem} (answer: ${show(actual)})`);
    }
  } finally {
    await service.client(id, "close", "POST");
  }
  return failures;
}

export async function runVectors(service: Service, only?: string): Promise<VectorOutcome> {
  const all = loadVectors().filter((vector) => only === undefined || vector.name.includes(only));
  const run = all.filter((vector) => !SKIPPED.has(vector.name));
  const groups = grouped(run);
  const server = new FakeServer("tgl_server_vectors_0");
  groups.forEach((group, index) =>
    server.setRuleset(group[0]?.flags, group[0]?.segments, `tgl_server_vectors_${index}`),
  );
  server.start();
  const failures: string[] = [];
  let next = 0;
  const worker = async (): Promise<void> => {
    while (next < groups.length) {
      const index = next++;
      failures.push(
        ...(await runGroup(service, server, `tgl_server_vectors_${index}`, groups[index] ?? [])),
      );
    }
  };
  try {
    await Promise.all(Array.from({ length: CONCURRENCY }, worker));
  } finally {
    await server.stop();
  }
  return { passed: run.length - failures.length, skipped: all.length - run.length, failures };
}
