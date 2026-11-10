/**
 * The harness's ToggleSlash server: the three SDK routes over real HTTP on 127.0.0.1, answering as
 * `docs/api.md` and `docs/architecture.md` say unless a scenario scripted the next answer, and recording
 * every request it gets.
 */
import type { Server } from "bun";
import { toJson } from "./json.ts";

export type Route = "ruleset" | "stream" | "events";

export interface Recorded {
  /** Milliseconds since the server was made, from a monotonic clock. */
  at: number;
  method: string;
  /** The path and query, as sent. */
  path: string;
  route: Route | "other";
  /** Header names lowercased. */
  headers: Record<string, string>;
  body: Uint8Array;
  /** The status answered, or null for a request left hanging. */
  answered: number | null;
}

/** One scripted answer to a route. */
export interface Answer {
  status: number;
  /** The body, sent as is; null sends the route's usual body for that status. */
  body: string | null;
  headers: Record<string, string>;
  /** Never answer; the request ends when the client gives up. */
  hang: boolean;
  /** Send the status, the headers and the start of a body, then nothing more. */
  hangBody: boolean;
  /** For the stream: these bytes in place of the `connected` event, each chunk its own write. */
  sse: Uint8Array[] | null;
  /** For `sse`: end the stream after the last chunk instead of keeping it open. */
  end: boolean;
  /** How many requests this answer serves before the next one in the script. */
  times: number;
}

interface Held {
  flags: unknown;
  segments: unknown;
  version: number;
}

const ROUTES: Record<string, Route> = {
  "GET /sdk/v1/ruleset": "ruleset",
  "GET /sdk/v1/stream": "stream",
  "POST /sdk/v1/events": "events",
};

/** The pause between two scripted chunks, so they leave as separate writes. */
const CHUNK_GAP_MS = 25;
const encoder = new TextEncoder();

function errorBody(code: string, message: string): string {
  return JSON.stringify({ error: { code, message } });
}

function eventCount(body: Uint8Array): number {
  try {
    const parsed: unknown = JSON.parse(new TextDecoder().decode(body));
    const events =
      typeof parsed === "object" && parsed !== null && "events" in parsed ? parsed.events : null;
    return Array.isArray(events) ? events.length : 0;
  } catch {
    return 0;
  }
}

export class FakeServer {
  readonly requests: Recorded[] = [];
  private readonly held = new Map<string, Held>();
  private readonly scripts = new Map<Route, Answer[]>();
  private readonly streams = new Set<ReadableStreamDefaultController<Uint8Array>>();
  private readonly hanging = new Set<() => void>();
  private readonly born = performance.now();
  private server: Server<undefined> | null = null;
  private port = 0;

  /** `key` is the scenario's server key; more keys, each with its own ruleset, can be added. */
  constructor(readonly key: string) {}

  get url(): string {
    return `http://127.0.0.1:${this.port}`;
  }

  get openStreams(): number {
    return this.streams.size;
  }

  now(): number {
    return performance.now() - this.born;
  }

  etag(key = this.key): string {
    return `"v${this.held.get(key)?.version ?? 0}"`;
  }

  /** Replaces the ruleset a key reads, and gives it a new ETag. */
  setRuleset(flags: unknown, segments: unknown, key = this.key): void {
    const version = (this.held.get(key)?.version ?? 0) + 1;
    this.held.set(key, { flags, segments, version });
  }

  script(route: Route, answers: Answer[]): void {
    this.scripts.set(route, [...(this.scripts.get(route) ?? []), ...answers]);
  }

  unscript(route: Route): void {
    this.scripts.delete(route);
  }

  start(): void {
    this.server = Bun.serve({
      hostname: "127.0.0.1",
      port: this.port,
      idleTimeout: 0,
      fetch: (request) => this.handle(request),
    });
    this.port = this.server.port ?? 0;
  }

  /** Closes the listener and every connection: the next request is refused. */
  async stop(): Promise<void> {
    for (const release of this.hanging) release();
    this.hanging.clear();
    this.dropStreams();
    await this.server?.stop(true);
    this.server = null;
  }

  /** Tells every open stream the ruleset changed. */
  publish(): void {
    this.write([encoder.encode(`event: changed\ndata: ${this.etag()}\n\n`)]);
  }

  /** Writes chunks to every open stream, a short pause apart. */
  async writeChunks(chunks: Uint8Array[]): Promise<void> {
    for (const [index, chunk] of chunks.entries()) {
      if (index > 0) await Bun.sleep(CHUNK_GAP_MS);
      this.write([chunk]);
    }
  }

  dropStreams(): void {
    for (const stream of this.streams) {
      try {
        stream.close();
      } catch {
        // Already closed by the client.
      }
    }
    this.streams.clear();
  }

  private write(chunks: Uint8Array[]): void {
    for (const stream of this.streams) for (const chunk of chunks) stream.enqueue(chunk);
  }

  private async handle(request: Request): Promise<Response> {
    const url = new URL(request.url);
    const seen: Recorded = {
      at: this.now(),
      method: request.method,
      path: url.pathname + url.search,
      route: ROUTES[`${request.method} ${url.pathname}`] ?? "other",
      headers: Object.fromEntries(request.headers),
      body: new Uint8Array(),
      answered: null,
    };
    this.requests.push(seen);
    seen.body = new Uint8Array(await request.arrayBuffer());
    const response = await this.answer(seen, request.signal);
    if (response !== null) seen.answered = response.status;
    return response ?? new Response(null, { status: 499 });
  }

  private async answer(seen: Recorded, signal: AbortSignal): Promise<Response | null> {
    const auth = seen.headers["authorization"] ?? "";
    const key = auth.startsWith("Bearer ") ? auth.slice("Bearer ".length) : "";
    if (seen.route === "other")
      return new Response(errorBody("not_found", "No such route."), { status: 404 });
    if (!this.held.has(key))
      return new Response(errorBody("unauthorized", "A valid key is required."), { status: 401 });
    const scripted = this.next(seen.route);
    if (scripted !== null) return this.scripted(seen.route, scripted, signal);
    if (seen.route === "ruleset") return this.ruleset(key, seen.headers["if-none-match"]);
    if (seen.route === "stream") return this.stream([encoder.encode(this.connected(key))], false);
    return Response.json({ accepted: eventCount(seen.body), unknownFlags: 0 });
  }

  private next(route: Route): Answer | null {
    const queue = this.scripts.get(route);
    const head = queue?.[0];
    if (queue === undefined || head === undefined) return null;
    head.times -= 1;
    if (head.times <= 0) queue.shift();
    return head;
  }

  private connected(key: string): string {
    return `event: connected\ndata: ${this.etag(key)}\n\n`;
  }

  private ruleset(key: string, ifNoneMatch: string | undefined): Response {
    const etag = this.etag(key);
    if (ifNoneMatch === etag) return new Response(null, { status: 304, headers: { ETag: etag } });
    const held = this.held.get(key);
    const body = toJson({ flags: held?.flags, segments: held?.segments });
    return new Response(body, {
      headers: { ETag: etag, "Content-Type": "application/json; charset=utf-8" },
    });
  }

  private scripted(
    route: Route,
    answer: Answer,
    signal: AbortSignal,
  ): Promise<Response | null> | Response {
    if (answer.hang) return this.hang(signal);
    if (answer.sse !== null) return this.stream(answer.sse, answer.end);
    if (answer.hangBody) return this.stalled(answer);
    const body = answer.body ?? defaultBody(route, answer.status);
    return new Response(body, { status: answer.status, headers: answer.headers });
  }

  private hang(signal: AbortSignal): Promise<Response | null> {
    return new Promise((resolve) => {
      const release = (): void => {
        this.hanging.delete(release);
        resolve(null);
      };
      this.hanging.add(release);
      signal.addEventListener("abort", release, { once: true });
    });
  }

  /** A body that starts and never finishes. */
  private stalled(answer: Answer): Response {
    let held: ReadableStreamDefaultController<Uint8Array> | undefined;
    const body = new ReadableStream<Uint8Array>({
      start: (controller) => {
        held = controller;
        controller.enqueue(encoder.encode(answer.body ?? '{"flags":['));
        this.streams.add(controller);
      },
      cancel: () => {
        if (held) this.streams.delete(held);
      },
    });
    return new Response(body, { status: answer.status, headers: answer.headers });
  }

  private stream(chunks: Uint8Array[], end: boolean): Response {
    let held: ReadableStreamDefaultController<Uint8Array> | undefined;
    const body = new ReadableStream<Uint8Array>({
      start: (controller) => {
        held = controller;
        this.streams.add(controller);
        void this.feed(controller, chunks, end);
      },
      cancel: () => {
        if (held) this.streams.delete(held);
      },
    });
    return new Response(body, {
      headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" },
    });
  }

  private async feed(
    controller: ReadableStreamDefaultController<Uint8Array>,
    chunks: Uint8Array[],
    end: boolean,
  ): Promise<void> {
    try {
      for (const [index, chunk] of chunks.entries()) {
        if (index > 0) await Bun.sleep(CHUNK_GAP_MS);
        if (!this.streams.has(controller)) return;
        controller.enqueue(chunk);
      }
      if (end) {
        this.streams.delete(controller);
        controller.close();
      }
    } catch {
      // The client went away mid-script.
    }
  }
}

function defaultBody(route: Route, status: number): string {
  if (status === 429) return errorBody("rate_limited", "Slow down.");
  if (route === "events" && status >= 200 && status < 300) return '{"accepted":0,"unknownFlags":0}';
  return status >= 400 ? errorBody("scripted", `Scripted ${status}.`) : "";
}
