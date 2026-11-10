import {
  createBrowserClient,
  type BrowserClient,
  type BrowserClientOptions,
} from "../src/index.ts";
import { FakeClock, settle } from "./fake-clock.ts";
import { type Answer, FakeServer } from "./fake-server.ts";

export const URL_BASE = "https://flags.example.com";

export interface Harness {
  clock: FakeClock;
  server: FakeServer;
  errors: unknown[];
  client: BrowserClient;
}

/** A client over the fake server and clock. It has not been given time to run: `await settle()` or `ready()`. */
export function start(
  answer: Answer,
  options: Partial<BrowserClientOptions> = {},
  prepare: (server: FakeServer) => void = () => undefined,
): Harness {
  const clock = new FakeClock();
  const server = new FakeServer(() => clock.now(), answer);
  // the client sends its first requests as it is made, so a test sets the server up first
  prepare(server);
  const errors: unknown[] = [];
  const client = createBrowserClient({
    url: URL_BASE,
    clientKey: server.key,
    context: { key: "alice" },
    fetch: server.fetch,
    clock,
    onError: (error) => errors.push(error),
    ...options,
  });
  return { clock, server, errors, client };
}

export async function started(
  answer: Answer,
  options: Partial<BrowserClientOptions> = {},
): Promise<Harness> {
  const harness = start(answer, options);
  await harness.client.ready();
  await settle();
  return harness;
}
