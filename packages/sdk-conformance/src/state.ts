/** What one scenario run holds while its steps go by, and the templates its steps may use. */
import { isRecord, type Rec } from "./json.ts";
import type { FakeServer } from "./server.ts";
import type { Service } from "./service.ts";

/** A point a later step can measure from: a request count, a time on both clocks, an error count. */
export interface Mark {
  seq: number;
  at: number;
  wall: number;
  errors: number;
}

/** The two values every context carries, which no request may ever contain. */
export interface Sentinel {
  key: string;
  attribute: string;
}

export interface RunState {
  server: FakeServer;
  service: Service;
  client: string | null;
  closed: boolean;
  marks: Map<string, Mark>;
  sentinel: Sentinel;
}

export function sentinelFor(scenario: number): Sentinel {
  const tag = `${scenario}-${crypto.randomUUID().slice(0, 8)}`;
  return { key: `ctx-sentinel-key-${tag}`, attribute: `ctx-sentinel-attr-${tag}` };
}

export function defaultContext(sentinel: Sentinel): Rec {
  return { key: sentinel.key, attributes: { sentinel: sentinel.attribute } };
}

/** Adds the sentinel attribute to a context a scenario wrote, single or multi, without changing anything else. */
export function withSentinel(context: unknown, sentinel: Sentinel): unknown {
  if (!isRecord(context)) return context;
  if (isRecord(context["contexts"])) {
    const contexts = Object.fromEntries(
      Object.entries(context["contexts"]).map(([kind, one]) => [kind, withSentinel(one, sentinel)]),
    );
    return { ...context, contexts };
  }
  const attributes = isRecord(context["attributes"]) ? context["attributes"] : {};
  if ("attributes" in context && !isRecord(context["attributes"])) return context;
  return { ...context, attributes: { ...attributes, sentinel: sentinel.attribute } };
}

export function today(): string {
  return new Date().toISOString().slice(0, 10);
}

/** Fills `{{url}}`, `{{key}}`, `{{etag}}`, `{{today}}` and `{{sentinel}}`; the string `{{context}}` alone is the default context. */
export function fill(value: unknown, state: RunState): unknown {
  if (typeof value === "string") return fillString(value, state);
  if (Array.isArray(value)) return value.map((item) => fill(item, state));
  if (isRecord(value))
    return Object.fromEntries(
      Object.entries(value).map(([name, item]) => [name, fill(item, state)]),
    );
  return value;
}

function fillString(value: string, state: RunState): unknown {
  if (value === "{{context}}") return defaultContext(state.sentinel);
  return value
    .replaceAll("{{url}}", state.server.url)
    .replaceAll("{{key}}", state.server.key)
    .replaceAll("{{etag}}", state.server.etag())
    .replaceAll("{{today}}", today())
    .replaceAll("{{sentinel}}", state.sentinel.key);
}

export function mark(state: RunState, name: string): Mark {
  const found = state.marks.get(name);
  if (found === undefined) throw new Error(`no mark named "${name}"`);
  return found;
}

export async function errorCount(state: RunState): Promise<number> {
  if (state.client === null) return 0;
  const answer = await state.service.client(state.client, "errors", "GET");
  return typeof answer["count"] === "number" ? answer["count"] : 0;
}
