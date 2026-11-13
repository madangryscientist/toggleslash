import { settle } from "./fake-clock.ts";

export interface Seen {
  method: string;
  path: string;
  url: string;
  headers: Headers;
  body: unknown;
  /** The fake clock's time when the request arrived. */
  at: number;
}

/** What the server sends for a flag: `ReasonResponse` is flat, with nulls. */
export interface WireResult {
  value: unknown;
  variationIndex: number | null;
  reason: { kind: string; ruleIndex: number | null; flagKey: string | null; code: string | null };
}

const NO_FIELDS = { ruleIndex: null, flagKey: null, code: null };

/** A flag served by its fallthrough: variation `index` with `value`. */
export function served(value: unknown, index = 0): WireResult {
  return { value, variationIndex: index, reason: { kind: "FALLTHROUGH", ...NO_FIELDS } };
}

export type Answer = Record<string, WireResult>;

const encoder = new TextEncoder();

/**
 * The client routes of the server, in memory: `evaluate` (an answer per context), `changes` (a stream)
 * and `events` (a sink). It checks the key as the real one does.
 */
export class FakeServer {
  readonly key = "tgl_client_test";
  /** What `evaluate` answers, per context key. */
  answers = new Map<string, Answer>();
  /** Answered when a context key has no entry in `answers`. Loosely typed so a test can send a bad answer. */
  fallback: Record<string, unknown> = {};
  down = false;
  /** Statuses by path, answered instead of the route. `retryAfter` goes in the `Retry-After` header. */
  readonly statuses = new Map<string, { status: number; retryAfter?: string }>();
  readonly seen: Seen[] = [];
  /** Evaluate requests wait for these, in order, after reading the state they answer. */
  private holds: Promise<void>[] = [];
  private streams = new Set<ReadableStreamDefaultController<Uint8Array>>();

  constructor(
    private readonly now: () => number,
    answer: Answer = {},
  ) {
    this.fallback = answer;
  }

  /** The next evaluate request answers from the state it saw, but only once the returned function is called. */
  hold(): () => void {
    const gate = new Gate();
    this.holds.push(gate.promise);
    return gate.open;
  }

  requestsTo(path: string): Seen[] {
    return this.seen.filter((seen) => seen.path === path);
  }

  get openStreams(): number {
    return this.streams.size;
  }

  get batches(): Record<string, unknown>[][] {
    return this.requestsTo("/sdk/v1/events").map((seen) => {
      return eventsOf(seen.body);
    });
  }

  publish(): void {
    for (const stream of this.streams) stream.enqueue(encoder.encode(event("changed")));
  }

  dropStreams(): void {
    for (const stream of this.streams) stream.close();
    this.streams.clear();
  }

  async goDown(): Promise<void> {
    this.down = true;
    this.dropStreams();
    await settle();
  }

  readonly fetch = async (input: string, init?: RequestInit): Promise<Response> => {
    if (this.down) throw new TypeError("fetch failed");
    const url = new URL(input);
    const headers = new Headers(init?.headers);
    const body = parseBody(init?.body);
    this.seen.push({
      method: init?.method ?? "GET",
      path: url.pathname,
      url: input,
      headers,
      body,
      at: this.now(),
    });
    if (headers.get("Authorization") !== `Bearer ${this.key}`)
      return new Response("", { status: 401 });
    const override = this.statuses.get(url.pathname);
    if (override) return overridden(override);
    return this.route(url.pathname, body, init?.signal ?? undefined);
  };

  private route(
    path: string,
    body: unknown,
    signal: AbortSignal | undefined,
  ): Promise<Response> | Response {
    if (path === "/sdk/v1/evaluate") return this.evaluate(body, signal);
    if (path === "/sdk/v1/changes") return this.changes(signal);
    if (path === "/sdk/v1/events")
      return new Response('{"accepted":1,"unknownFlags":0}', { status: 200 });
    return new Response("", { status: 404 });
  }

  private async evaluate(body: unknown, signal: AbortSignal | undefined): Promise<Response> {
    const answer = this.answers.get(contextKey(body)) ?? this.fallback;
    const text = JSON.stringify({ flags: answer });
    const hold = this.holds.shift();
    if (hold) {
      await new Promise<void>((resolve, reject) => {
        void hold.then(resolve);
        signal?.addEventListener("abort", () => reject(signal.reason), { once: true });
      });
    }
    return new Response(text, { status: 200 });
  }

  private changes(signal: AbortSignal | undefined): Response {
    let controller: ReadableStreamDefaultController<Uint8Array> | undefined;
    const body = new ReadableStream<Uint8Array>({
      start: (started) => {
        controller = started;
        this.streams.add(started);
        started.enqueue(encoder.encode(event("connected")));
      },
    });
    signal?.addEventListener(
      "abort",
      () => {
        if (!controller || !this.streams.delete(controller)) return;
        controller.error(signal.reason);
      },
      { once: true },
    );
    return new Response(body, { headers: { "Content-Type": "text/event-stream" } });
  }
}

function event(name: string): string {
  return `event: ${name}\ndata: {}\n\n`;
}

/** A promise a test opens by hand. */
class Gate {
  readonly promise: Promise<void>;
  open: () => void = () => undefined;

  constructor() {
    this.promise = new Promise<void>((resolve) => {
      this.open = resolve;
    });
  }
}

function overridden(override: { status: number; retryAfter?: string }): Response {
  const headers = override.retryAfter === undefined ? {} : { "Retry-After": override.retryAfter };
  return new Response("", { status: override.status, headers });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function contextKey(body: unknown): string {
  const context = isRecord(body) ? body["context"] : undefined;
  const key = isRecord(context) ? context["key"] : undefined;
  return typeof key === "string" ? key : "";
}

function eventsOf(body: unknown): Record<string, unknown>[] {
  const events = isRecord(body) ? body["events"] : undefined;
  return Array.isArray(events) ? events.filter(isRecord) : [];
}

function parseBody(raw: unknown): unknown {
  return typeof raw === "string" ? JSON.parse(raw) : undefined;
}
