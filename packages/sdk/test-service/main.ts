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

interface Entry {
  client: Client;
  hookEvents: EvaluationEvent[];
  errors: string[];
}

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

function clientOptions(body: Record<string, unknown>, entry: Omit<Entry, "client">): ClientOptions {
  const options: ClientOptions = {
    url: String(body["url"]),
    sdkKey: String(body["sdkKey"]),
    onError: (error) => {
      entry.errors.push(error instanceof Error ? error.message : String(error));
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
  if (body["hook"] === true || body["hookThrows"] === true) {
    options.hooks = {
      afterEvaluation: (event) => {
        entry.hookEvents.push(event);
        if (body["hookThrows"] === true) throw new Error("the hook threw, as the test asked");
      },
    };
  }
  return options;
}

async function create(request: Request): Promise<Response> {
  const body = await readBody(request);
  const recorded = { hookEvents: [], errors: [] };
  const id = String(++created);
  clients.set(id, { ...recorded, client: createClient(clientOptions(body, recorded)) });
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
