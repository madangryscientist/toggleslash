import { defineConfig } from "vitest/config";

// Runs under the Bun runtime (`bun --bun vitest`): the harness uses Bun.serve. The whole run is one test.
export default defineConfig({
  test: {
    environment: "node",
    include: ["test/**/*.test.ts"],
    globalSetup: ["../../scripts/test-tmp-scope.ts"],
    testTimeout: 180_000,
  },
});
