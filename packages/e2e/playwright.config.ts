import { defineConfig, devices } from "@playwright/test";

// One browser journey through the whole real stack (`startStack()`: the C# server, the seed, the
// dashboard and the example app), started once by `stack/setup.ts`. Run it with `bun run test:stack`
// (it needs Bun as the runtime, which `bun --bun playwright` gives it). `test-tmp-scope.ts` goes
// first so the stack's scratch and the browser's profile live under one TMPDIR that is removed after.
export default defineConfig({
  testDir: "./stack",
  testMatch: /.*\.spec\.ts/,
  globalSetup: ["../../scripts/test-tmp-scope.ts", "./stack/setup.ts"],
  forbidOnly: !!process.env["CI"],
  retries: 0, // a flaky journey is a bug to fix, not to retry
  workers: 1,
  reporter: [["list"], ["./stack/logs-reporter.ts"]],
  timeout: 180_000,
  expect: { timeout: 15_000 },
  use: { ...devices["Desktop Chrome"] },
});
