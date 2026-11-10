import { join } from "node:path";

/**
 * Builds `src/browser.ts` and what it imports into one ESM file for the page, in memory. Nothing is
 * written to disk. Throws with the bundler's messages when the build fails, so startup stops.
 */
export async function buildBrowserBundle(): Promise<string> {
  const result = await Bun.build({
    entrypoints: [join(import.meta.dirname, "browser.ts")],
    target: "browser",
    format: "esm",
    minify: false,
  });
  if (!result.success) {
    throw new Error(`the browser bundle did not build:\n${result.logs.map(String).join("\n")}`);
  }
  const [output] = result.outputs;
  if (output === undefined || result.outputs.length !== 1) {
    throw new Error(`the browser bundle built ${result.outputs.length} files, not one`);
  }
  return await output.text();
}
