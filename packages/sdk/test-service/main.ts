/**
 * The TypeScript SDK's conformance test service: an HTTP server that wraps `createClient` so the harness in
 * `packages/sdk-conformance` can drive it from outside. The protocol is `docs/sdk-test-service.md`; the
 * accessor mapping is in this directory's README. It catches nothing the SDK throws: a call that raises
 * answers 500 with the error text, and the harness fails the scenario.
 */
import type { Server } from "bun";
import {
  type Client,
  type ClientOptions,
  type Context,
  createClient,
  type EvaluationEvent,
} from "../src/index.ts";

interface CallbackClose {
  calls: number;
  threw: number;
  maxMs: number;
}

interface Entry {
  client: Client;
  hookEvents: EvaluationEvent[];
  errors: string[];
  callbackClose: CallbackClose;
}

/** `closing` is set when the callback starts its close; `callbackClose.calls` once that close returned. */
type Recorded = Omit<Entry, "client"> & { client: Client | null; closing: boolean };

const clients = new Map<string, Entry>();
let created = 0;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/**
 * The service hands the SDK whatever JSON the harness sent, valid or not: an invalid context is one of the
 * cases the SDK must answer (`ERROR INVALID_CONTEXT`), so nothing here checks it.
 */
function passedThrough(_value: unknown): _value is Context {
  return true;
}

async function readBody(request: Request): Promise<Record<string, unknown>> {
  const text = await request.text();
  const body: unknown = text === "" ? {} : JSON.parse(text);
  if (!isRecord(body)) throw new TypeError("the body is not a JSON object");
  return body;
}

function numberOption(body: Record<string, unknown>, name: string): number | undefined {
  const value = body[name];
  return typeof value === "number" ? value : undefined;
}

/**
 * `closeInHook` and `closeInOnError`: the first callback that finds the client calls `close` and times how
 * long the call took to return. The promise is not awaited: an async close returns at once by design.
 */
function closeFromCallback(entry: Recorded): void {
  if (entry.client === null || entry.closing) return;
  entry.closing = true;
  const started = performance.now();
  let threw = 0;
  try {
    void entry.client.close();
  } catch {
    threw = 1;
  }
  entry.callbackClose.maxMs = Math.round(performance.now() - started);
  entry.callbackClose.threw = threw;
  entry.callbackClose.calls = 1;
}

function clientOptions(body: Record<string, unknown>, entry: Recorded): ClientOptions {
  const options: ClientOptions = {
    url: String(body["url"]),
    sdkKey: String(body["sdkKey"]),
    onError: (error) => {
      entry.errors.push(error instanceof Error ? error.message : String(error));
      if (body["closeInOnError"] === true) closeFromCallback(entry);
      if (body["onErrorThrows"] === true) throw new Error("onError threw, as the test asked");
    },
  };
  if (typeof body["stream"] === "boolean") options.stream = body["stream"];
  const poll = numberOption(body, "pollIntervalMs");
  if (poll !== undefined) options.pollIntervalMs = poll;
  const flush = numberOption(body, "flushIntervalMs");
  if (flush !== undefined) options.flushIntervalMs = flush;
  const timeout = numberOption(body, "requestTimeoutMs");
  if (timeout !== undefined) options.requestTimeoutMs = timeout;
  if (body["hook"] === true || body["hookThrows"] === true || body["closeInHook"] === true) {
    options.hooks = {
      afterEvaluation: (event) => {
        entry.hookEvents.push(event);
        if (body["closeInHook"] === true) closeFromCallback(entry);
        if (body["hookThrows"] === true) throw new Error("the hook threw, as the test asked");
      },
    };
  }
  return options;
}

async function create(request: Request): Promise<Response> {
  const body = await readBody(request);
  const recorded: Recorded = {
    client: null,
    closing: false,
    hookEvents: [],
    errors: [],
    callbackClose: { calls: 0, threw: 0, maxMs: 0 },
  };
  const id = String(++created);
  const client = createClient(clientOptions(body, recorded));
  recorded.client = client;
  clients.set(id, { ...recorded, client });
  return Response.json({ id }, { status: 201 });
}

/** Every accessor is `variationDetail` with the fallback given: TypeScript has one generic method. */
async function evaluate(entry: Entry, request: Request): Promise<Response> {
  const body = await readBody(request);
  const context = body["context"];
  if (!passedThrough(context)) throw new TypeError("unreachable");
  const detail = entry.client.variationDetail(String(body["flagKey"]), context, body["fallback"]);
  return Response.json({
    value: detail.value,
    variationIndex: detail.variationIndex,
    reason: detail.reason,
  });
}

async function clientRoute(entry: Entry, action: string, request: Request): Promise<Response> {
  const key = `${request.method} ${action}`;
  if (key === "POST ready") return Response.json({ ready: await entry.client.ready() });
  if (key === "POST evaluate") return evaluate(entry, request);
  if (key === "GET status") return Response.json(entry.client.status());
  if (key === "GET hook-events") return Response.json({ events: entry.hookEvents });
  if (key === "GET callback-close") return Response.json(entry.callbackClose);
  if (key === "GET errors")
    return Response.json({ count: entry.errors.length, messages: entry.errors });
  if (key === "POST close") {
    await entry.client.close();
    return Response.json({});
  }
  return new Response("no such route", { status: 404 });
}

async function route(request: Request): Promise<Response> {
  const { pathname } = new URL(request.url);
  if (request.method === "GET" && pathname === "/")
    return Response.json({ name: "toggleslash-sdk (TypeScript)", capabilities: [] });
  if (request.method === "POST" && pathname === "/clients") return create(request);
  const match = /^\/clients\/([^/]+)\/([a-z-]+)$/.exec(pathname);
  const entry = clients.get(match?.[1] ?? "");
  if (match === null || entry === undefined) return new Response("no such client", { status: 404 });
  return clientRoute(entry, match[2] ?? "", request);
}

const server: Server<undefined> = Bun.serve({
  hostname: "127.0.0.1",
  port: 0,
  idleTimeout: 0,
  fetch: async (request) => {
    try {
      return await route(request);
    } catch (error) {
      return new Response(error instanceof Error ? (error.stack ?? error.message) : String(error), {
        status: 500,
      });
    }
  },
});

process.stdout.write(`listening http://127.0.0.1:${server.port}\n`);
