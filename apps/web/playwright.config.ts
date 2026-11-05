import { defineConfig, devices } from "@playwright/test";
import { API_PORT, E2E_ADMIN_TOKEN } from "./e2e/constants.ts";

const PORT = 4420;

// A local production build, with a fake admin API (`e2e/fake-api.ts`) standing in for the C# server,
// so the run needs no .NET SDK. The build still gets an empty local D1 (`.wrangler/e2e`).
export default defineConfig({
  testDir: "./e2e",
  globalSetup: "../../scripts/test-tmp-scope.ts",
  forbidOnly: !!process.env["CI"],
  reporter: "list",
  // One local D1 holds every spec's rows: concurrent writers fail with SQLITE_BUSY.
  workers: 1,
  use: { baseURL: `http://localhost:${PORT}`, trace: "on-first-retry" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    {
      command: "bun e2e/fake-api.ts",
      url: `http://127.0.0.1:${API_PORT}/health`,
      reuseExistingServer: false,
      timeout: 60_000,
    },
    {
      command: [
        "rm -rf .wrangler/e2e",
        "mkdir -p .wrangler/e2e/tmp",
        "bun run build",
        "wrangler d1 migrations apply DB --local --env e2e --persist-to .wrangler/e2e",
        `vite preview --port ${PORT} --strictPort`,
      ].join(" && "),
      env: {
        FEATURESLASH_E2E: "1",
        CLOUDFLARE_ENV: "e2e",
        // The web server starts before `globalSetup` scopes TMPDIR, so it gets its own under
        // .wrangler/e2e, which the next run clears: miniflare and wrangler leave nothing in /tmp.
        TMPDIR: ".wrangler/e2e/tmp",
        // Lets the Worker read these from the process environment, as a deployed one reads its secrets.
        // It binds this process's whole environment into the preview Worker, which only the dashboard's
        // two FEATURESLASH_* reads touch; the token is a throwaway (e2e/constants.ts), never a real one.
        CLOUDFLARE_INCLUDE_PROCESS_ENV: "true",
        FEATURESLASH_API_URL: `http://127.0.0.1:${API_PORT}`,
        FEATURESLASH_ADMIN_TOKEN: E2E_ADMIN_TOKEN,
      },
      url: `http://localhost:${PORT}`,
      reuseExistingServer: false,
      timeout: 300_000,
    },
  ],
});
