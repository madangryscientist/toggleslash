import { defineConfig, devices } from "@playwright/test";

// One browser journey through the whole real stack (`startStack()`: the C# server, the seed, the
// dashboard and the example app), started once by `stack/setup.ts`. Run it with `bun run test:stack`
// (it needs Bun as the runtime, which `bun --bun playwright` gives it). `test-tmp-scope.ts` goes
// first so the stack's scratch, the saved session and the browser's profile live under one TMPDIR
// that is removed after.
export default defineConfig({
  testDir: "./stack",
  globalSetup: ["../../scripts/test-tmp-scope.ts", "./stack/setup.ts"],
  forbidOnly: !!process.env["CI"],
  retries: 0, // a flaky journey is a bug to fix, not to retry
  workers: 1,
  reporter: [["list"], ["./stack/logs-reporter.ts"]],
  // The journey took 2.4 s here (2026-11-15), and the old ten-step one 5.3 s on CI's runner: a
  // limit of ten times the slower still ends a hung run in a minute. The stack starts in globalSetup,
  // outside it.
  timeout: 60_000,
  // The longest wait is from the save to the browser SDK's table repainting (the stream, then the
  // change signal and a fresh evaluate), well under the whole 2.4 s here; 15 s leaves CI room.
  expect: { timeout: 15_000 },
  use: { ...devices["Desktop Chrome"] },
  projects: [
    // Signs in through the real form once; the journey starts from the session it saves.
    { name: "setup", testMatch: /sign-in\.setup\.ts/ },
    { name: "journey", testMatch: /journey\.spec\.ts/, dependencies: ["setup"] },
  ],
});
