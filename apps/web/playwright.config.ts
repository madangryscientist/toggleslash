import { defineConfig, devices } from "@playwright/test";
import {
  API_PORT,
  E2E_ADMIN_TOKEN,
  E2E_OPERATOR_PASSWORD,
  E2E_SESSION_SECRET,
  PORT,
  SIGNED_IN_STATE,
  UNCONFIGURED_PORT,
} from "./e2e/constants.ts";

const WORKER_ENV = {
  TOGGLESLASH_E2E: "1",
  CLOUDFLARE_ENV: "e2e",
  // The web server starts before `globalSetup` scopes TMPDIR, so it gets its own under
  // .wrangler/e2e, which the next run clears: miniflare and wrangler leave nothing in /tmp.
  TMPDIR: ".wrangler/e2e/tmp",
  // Lets the Worker read these from the process environment, as a deployed one reads its secrets.
  // It binds this process's whole environment into the preview Worker, which only the dashboard's
  // TOGGLESLASH_* reads touch; every value is a throwaway (e2e/constants.ts), never a real one.
  CLOUDFLARE_INCLUDE_PROCESS_ENV: "true",
  TOGGLESLASH_API_URL: `http://127.0.0.1:${API_PORT}`,
  TOGGLESLASH_ADMIN_TOKEN: E2E_ADMIN_TOKEN,
};

// A local production build, with a fake admin API (`e2e/fake-api.ts`) standing in for the C# server,
// so the run needs no .NET SDK. Its local state stays apart from `vite dev`'s (`.wrangler/e2e`).
export default defineConfig({
  testDir: "./e2e",
  globalSetup: "../../scripts/test-tmp-scope.ts",
  forbidOnly: !!process.env["CI"],
  // A test that fails is a finding, never retried past.
  retries: 0,
  reporter: "list",
  // Every spec shares one fake API and its state, so they run one at a time.
  workers: 1,
  use: { baseURL: `http://localhost:${PORT}`, trace: "retain-on-failure" },
  projects: [
    // Signs in through the real form; every spec starts from the session it saves.
    { name: "setup", testMatch: /sign-in\.setup\.ts/, use: { ...devices["Desktop Chrome"] } },
    {
      name: "chromium",
      dependencies: ["setup"],
      use: { ...devices["Desktop Chrome"], storageState: SIGNED_IN_STATE },
    },
  ],
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
        // Bound at preview, not at build. The build copies its variables to dist/server/.dev.vars (the
        // whole environment with this on, or a local apps/web/.dev.vars), and that file would beat what
        // each preview server sets below.
        "CLOUDFLARE_INCLUDE_PROCESS_ENV=false bun run build",
        "rm -f dist/server/.dev.vars",
        `vite preview --port ${PORT} --strictPort`,
      ].join(" && "),
      env: {
        ...WORKER_ENV,
        TOGGLESLASH_OPERATOR_PASSWORD: E2E_OPERATOR_PASSWORD,
        TOGGLESLASH_SESSION_SECRET: E2E_SESSION_SECRET,
      },
      url: `http://localhost:${PORT}`,
      reuseExistingServer: false,
      timeout: 300_000,
    },
    {
      // The same build with neither secret set, but with the API and its token: if anything fell open,
      // this is where it would show flags. Playwright starts servers in order, so the build is done.
      command: `vite preview --port ${UNCONFIGURED_PORT} --strictPort`,
      env: {
        ...WORKER_ENV,
        TOGGLESLASH_OPERATOR_PASSWORD: "",
        TOGGLESLASH_SESSION_SECRET: "",
      },
      url: `http://localhost:${UNCONFIGURED_PORT}`,
      reuseExistingServer: false,
      timeout: 60_000,
    },
  ],
});
