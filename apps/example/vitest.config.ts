import { defineConfig } from "vitest/config";

// Runs under the Bun runtime (`bun --bun vitest`).
export default defineConfig({
  test: {
    environment: "node",
    include: ["test/**/*.test.ts"],
    globalSetup: ["../../scripts/test-tmp-scope.ts"],
  },
});
