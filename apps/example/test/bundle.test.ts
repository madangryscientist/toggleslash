import { describe, expect, it } from "vitest";
import { buildBrowserBundle } from "../src/bundle.ts";

const SERVER_KEY = "tgl_server_a-key-that-must-stay-on-the-server";

describe("buildBrowserBundle", () => {
  it("builds one file that runs the browser SDK and holds no server key", async () => {
    const bundle = await buildBrowserBundle();
    expect(bundle).toContain("createBrowserClient");
    expect(bundle).not.toContain(SERVER_KEY);
    expect(bundle).not.toContain("TOGGLESLASH_SDK_KEY");
  });
});
