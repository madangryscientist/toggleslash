import { settle } from "./fake-clock.ts";

export interface Seen {
  method: string;
  path: string;
  headers: Headers;
  body: unknown;
  /** The `redirect` mode the request asked for, if it named one. */
  redirect: RequestInit["redirect"];
}

/** A boolean flag, on, that serves `serves` to everyone. Variation 0 is false, 1 is true. */
export function flag(key: string, serves: boolean): Record<string, unknown> {
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

const encoder = new TextEncoder();

/**
 * The SDK routes of the server, in memory: a ruleset with an ETag, a stream, and an events sink. It
 * answers like the real one (`docs/architecture.md`), and checks the key.
 */
export class FakeServer {
  readonly key = "tgl_server_test";
  flags: unknown[];
  version = 1;
  down = false;
  streamStatus = 200;
  rulesetBody: string | null = null;
  eventsStatus = 200;
  eventsHang = false;
  /** Paths answering 429, with the `Retry-After` to send; null sends none. */
  readonly limited = new Map<string, string | null>();
  /** Paths answering this 3xx status, with a `Location` of `/elsewhere`. */
  readonly redirected = new Map<string, number>();
  readonly seen: Seen[] = [];
  private streams = new Set<ReadableStreamDefaultController<Uint8Array>>();

  constructor(flags: unknown[]) {
    this.flags = flags;
  }

  get etag(): string {
    return `"v${this.version}"`;
  }

  requestsTo(path: string): Seen[] {
    return this.seen.filter((seen) => seen.path === path);
  }

  get batches(): Record<string, unknown>[][] {
    return this.requestsTo("/sdk/v1/events").map((seen) => {
      const events = isRecord(seen.body) ? seen.body["events"] : undefined;
      return Array.isArray(events) ? events.filter(isRecord) : [];
    });
  }

  get openStreams(): number {
    return this.streams.size;
  }

  setFlags(flags: unknown[]): void {
    this.flags = flags;
    this.version++;
  }

  /** Tells every open stream the ruleset changed. */
  publish(): void {
    for (const stream of this.streams) stream.enqueue(encoder.encode(this.event("changed")));
  }

  write(text: string): void {
    for (const stream of this.streams) stream.enqueue(encoder.encode(text));
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

  private event(name: string): string {
    return `event: ${name}\ndata: ${this.etag}\n\n`;
  }

  readonly fetch = async (input: string, init?: RequestInit): Promise<Response> => {
    if (this.down) throw new TypeError("fetch failed");
    const path = new URL(input).pathname;
    const headers = new Headers(init?.headers);
    this.record(path, headers, init);
    if (headers.get("Authorization") !== `Bearer ${this.key}`)
      return new Response("", { status: 401 });
    return this.refused(path) ?? this.route(path, headers, init?.signal ?? undefined);
  };

  private record(path: string, headers: Headers, init: RequestInit | undefined): void {
    const { method = "GET", body, redirect } = init ?? {};
    this.seen.push({ method, path, headers, body: parsedBody(body), redirect });
  }

  /** A scripted redirect or 429 for this path, or null. */
  private refused(path: string): Response | null {
    const redirect = this.redirected.get(path);
    if (redirect !== undefined)
      return new Response("", { status: redirect, headers: { Location: "/elsewhere" } });
    return this.tooMany(path);
  }

  private tooMany(path: string): Response | null {
    const retryAfter = this.limited.get(path);
    if (retryAfter === undefined) return null;
    return new Response('{"error":{"code":"rate_limited","message":"Slow down."}}', {
      status: 429,
      headers: retryAfter === null ? {} : { "Retry-After": retryAfter },
    });
  }

  private route(
    path: string,
    headers: Headers,
    signal: AbortSignal | undefined,
  ): Promise<Response> | Response {
    if (path === "/sdk/v1/ruleset") return this.ruleset(headers);
    if (path === "/sdk/v1/stream") return this.stream(signal);
    if (path === "/sdk/v1/events") return this.events(signal);
    return new Response("", { status: 404 });
  }

  private ruleset(headers: Headers): Response {
    if (headers.get("If-None-Match") === this.etag) return new Response(null, { status: 304 });
    const text = this.rulesetBody ?? JSON.stringify({ flags: this.flags, segments: [] });
    return new Response(text, { status: 200, headers: { ETag: this.etag } });
  }

  private stream(signal: AbortSignal | undefined): Response {
    if (this.streamStatus !== 200) return new Response("", { status: this.streamStatus });
    let controller: ReadableStreamDefaultController<Uint8Array> | undefined;
    const body = new ReadableStream<Uint8Array>({
      start: (started) => {
        controller = started;
        this.streams.add(started);
        started.enqueue(encoder.encode(this.event("connected")));
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

  private events(signal: AbortSignal | undefined): Promise<Response> | Response {
    if (!this.eventsHang)
      return new Response('{"accepted":1,"unknownFlags":0}', { status: this.eventsStatus });
    return new Promise<Response>((_resolve, reject) => {
      signal?.addEventListener("abort", () => reject(signal.reason), { once: true });
    });
  }
}

function parsedBody(raw: unknown): unknown {
  return typeof raw === "string" ? JSON.parse(raw) : undefined;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
