import { afterEach, beforeEach } from "vitest";
import { type Client, type ClientOptions, createClient } from "../src/index.ts";
import { FakeClock, settle } from "./fake-clock.ts";
import { FakeServer, flag } from "./fake-server.ts";

export const user = { key: "user-1" };

export interface Harness {
  clock: FakeClock;
  server: FakeServer;
  /** What the client reported through `onError`. */
  errors: unknown[];
  start: (overrides?: Partial<ClientOptions>) => Client;
}

/**
 * Registers hooks that give each test a fresh fake clock and server, close every client it started, and
 * fail the test if anything rejected without a handler.
 */
export function useHarness(): Harness {
  let clients: Client[] = [];
  let unhandled: unknown[] = [];
  const onUnhandled = (reason: unknown): void => {
    unhandled.push(reason);
  };
  const harness: Harness = {
    clock: new FakeClock(),
    server: new FakeServer([]),
    errors: [],
    start: (overrides = {}) => {
      const client = createClient({
        url: "https://flags.example.test/",
        sdkKey: harness.server.key,
        fetch: harness.server.fetch,
        clock: harness.clock,
        random: () => 1,
        onError: (error) => harness.errors.push(error),
        ...overrides,
      });
      clients.push(client);
      return client;
    },
  };

  beforeEach(() => {
    harness.clock = new FakeClock();
    harness.server = new FakeServer([flag("checkout", true), flag("legacy", false)]);
    harness.errors = [];
    clients = [];
    unhandled = [];
    process.on("unhandledRejection", onUnhandled);
  });

  afterEach(async () => {
    const closing = Promise.all(clients.map((client) => client.close()));
    await harness.clock.advance(10_000);
    await closing;
    await settle();
    process.off("unhandledRejection", onUnhandled);
    if (unhandled.length > 0)
      throw new Error(`unhandled rejections: ${unhandled.map(String).join("; ")}`);
  });

  return harness;
}
