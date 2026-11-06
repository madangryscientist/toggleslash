import { readFileSync } from "node:fs";
import { describe, expect, it, vi } from "vitest";
import pkg from "../package.json" with { type: "json" };
import { run } from "../src/cli.ts";
import { COMMANDS } from "../src/commands/index.ts";
import { cli } from "./harness.ts";

describe("run", () => {
  it("prints the CLI package version for --version, through process.stdout", async () => {
    const write = vi.spyOn(process.stdout, "write").mockImplementation(() => true);
    try {
      expect(await run(["--version"])).toBe(0);
      expect(String(write.mock.calls[0]?.[0])).toContain(`toggleslash ${pkg.version}`);
    } finally {
      write.mockRestore();
    }
  });

  it("writes errors through process.stderr when no deps are injected", async () => {
    const write = vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    try {
      expect(await run(["nope"])).toBe(2);
      expect(String(write.mock.calls[0]?.[0])).toContain('unknown argument "nope"');
    } finally {
      write.mockRestore();
    }
  });

  it("exits 0 for --help and for no arguments", async () => {
    expect((await cli(["--help"])).code).toBe(0);
    expect((await cli([])).code).toBe(0);
    expect((await cli(["-h"])).stdout).toBe((await cli([])).stdout);
  });

  it("prints help for --help after a command, without running it", async () => {
    const result = await cli(["flags", "list", "--help"]);
    expect(result.code).toBe(0);
    expect(result.stdout).toContain("Exit codes:");
  });

  it("exits 2 for an unknown argument", async () => {
    const result = await cli(["nope"]);
    expect(result.code).toBe(2);
    expect(result.stdout).toBe("");
  });
});

describe("--help and README.md list every command", () => {
  const readme = readFileSync(new URL("../README.md", import.meta.url), "utf8");

  it.each(COMMANDS.map((c) => c.name))("%s", async (name) => {
    expect((await cli(["--help"])).stdout).toContain(`toggleslash ${name}`);
    expect(readme).toContain(`toggleslash ${name}`);
  });

  it("documents the three exit codes", async () => {
    const { stdout } = await cli(["--help"]);
    for (const text of [readme, stdout]) {
      expect(text).toMatch(/\b0\b.*success/i);
      expect(text).toMatch(/\b1\b.*(server|network)/i);
      expect(text).toMatch(/\b2\b.*usage/i);
    }
  });
});
