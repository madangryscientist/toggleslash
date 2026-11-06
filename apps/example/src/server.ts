import { createClient } from "@hackcove/toggleslash-sdk";
import { createApp } from "./app.ts";

const DEFAULT_PORT = 4441;

function required(name: string): string {
  const value = process.env[name];
  if (value === undefined || value === "") {
    process.stderr.write(`example: ${name} is not set\n`);
    process.exit(2);
  }
  return value;
}

/** An optional whole number of milliseconds; anything else is a mistake worth stopping for. */
function optionalMs(name: string): number | undefined {
  const raw = process.env[name];
  if (raw === undefined || raw === "") return undefined;
  const value = Number(raw);
  if (!Number.isInteger(value) || value < 1) {
    process.stderr.write(`example: ${name} must be a whole number of milliseconds\n`);
    process.exit(2);
  }
  return value;
}

const url = required("TOGGLESLASH_URL");
const flushIntervalMs = optionalMs("TOGGLESLASH_FLUSH_INTERVAL_MS");

// The server key goes to the SDK and nowhere else; `createApp` is never given it.
const client = createClient({
  url,
  sdkKey: required("TOGGLESLASH_SDK_KEY"),
  ...(flushIntervalMs === undefined ? {} : { flushIntervalMs }),
  onError: (error) => process.stderr.write(`example: sdk: ${String(error)}\n`),
});

const server = Bun.serve({
  // Loopback only: this is an example, not something to expose.
  hostname: "127.0.0.1",
  port: Number(process.env["PORT"] ?? DEFAULT_PORT),
  fetch: createApp({ client, serverUrl: url, clientKey: required("TOGGLESLASH_CLIENT_KEY") }),
});
process.stdout.write(`example: listening on http://127.0.0.1:${server.port}\n`);

let stopping = false;
async function stop(): Promise<void> {
  if (stopping) return;
  stopping = true;
  await server.stop();
  await client.close(); // sends the last usage counts
  process.exit(0);
}
process.on("SIGTERM", () => void stop());
process.on("SIGINT", () => void stop());
