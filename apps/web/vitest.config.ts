import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const DOM_TESTS = "src/**/*.dom.test.tsx";

// Separate from vite.config.ts so tests do not boot the Start and Cloudflare plugins.
export default defineConfig({
  resolve: { alias: { "#": fileURLToPath(new URL("./src", import.meta.url)) } },
  test: {
    globalSetup: ["../../scripts/test-tmp-scope.ts"],
    projects: [
      {
        extends: true,
        test: {
          name: "node",
          environment: "node",
          include: ["src/**/*.test.{ts,tsx}"],
          exclude: [DOM_TESTS],
        },
      },
      {
        // Components rendered in a DOM, with the dashboard's server functions mocked per test.
        extends: true,
        test: {
          name: "dom",
          environment: "happy-dom",
          include: [DOM_TESTS],
          setupFiles: ["src/test/setup-dom.ts"],
        },
      },
    ],
  },
});
