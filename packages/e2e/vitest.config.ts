import { defineConfig } from "vitest/config";

// Runs under the Bun runtime (`bun --bun vitest`). The test builds and starts the C# server, so the
// timeouts are generous; a cold `dotnet build` restores packages first.
export default defineConfig({
  test: {
    environment: "node",
    include: ["test/**/*.test.ts"],
    globalSetup: ["../../scripts/test-tmp-scope.ts"],
    // Each test file builds the C# server, and two concurrent `dotnet build` runs of one project collide.
    fileParallelism: false,
    hookTimeout: 600_000,
    testTimeout: 120_000,
  },
});
