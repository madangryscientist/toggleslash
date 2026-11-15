import { cloudflare } from "@cloudflare/vite-plugin";
import tailwindcss from "@tailwindcss/vite";
import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import viteReact from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Set by the Playwright run and the stack: no debugger port (below).
const e2e = process.env["TOGGLESLASH_E2E"] === "1";

export default defineConfig({
  resolve: { tsconfigPaths: true },
  plugins: [
    cloudflare({
      viteEnvironment: { name: "ssr" },
      // The plugin probes for a free debugger port starting at 9229 and two runs probing at once
      // both get it (EADDRINUSE). Nothing in e2e attaches a debugger.
      ...(e2e ? { inspectorPort: false } : {}),
    }),
    tailwindcss(),
    tanstackStart(),
    viteReact(),
  ],
});
