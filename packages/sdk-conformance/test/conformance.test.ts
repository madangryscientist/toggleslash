import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { failed, runAll, type Summary, summaryLine } from "../src/run.ts";
import { isRecord } from "../src/json.ts";
import { loadScenarios } from "../src/scenarios.ts";
import { type Started, startService } from "../src/service.ts";
import { STEPS } from "../src/steps.ts";

const CAPABILITIES = ["integer-accessor"];

describe("the scenarios in fixtures/sdk", () => {
  const scenarios = loadScenarios();

  it("have unique names, a contract section and only known steps and capabilities", () => {
    const names = scenarios.map((scenario) => `${scenario.file}: ${scenario.name}`);
    expect(new Set(names).size).toBe(names.length);
    const wrong = scenarios.flatMap((scenario) => {
      const steps = scenario.steps.map((step) =>
        isRecord(step) && typeof step["do"] === "string" ? step["do"] : "?",
      );
      const problems = [
        /^\d+(\.\d+)?$/.test(scenario.contract) ? [] : [`contract ${scenario.contract}`],
        steps.length > 0 ? [] : ["no steps"],
        scenario.requires.filter((name) => !CAPABILITIES.includes(name)),
        steps.filter((name) => !(name in STEPS)).map((name) => `step ${name}`),
      ].flat();
      return problems.map((problem) => `${scenario.name}: ${problem}`);
    });
    expect(wrong).toEqual([]);
  });

  it("cover every area", () => {
    const files = new Set(scenarios.map((scenario) => scenario.file.replace(/\.json$/, "")));
    const areas = ["load", "evaluation", "stream", "backoff", "rate-limit", "status", "usage"];
    for (const area of [...areas, "hooks", "close", "privacy"]) expect(files).toContain(area);
  });
});

describe("startService", () => {
  it("fails with the service's stderr when it exits before it listens", async () => {
    await expect(startService("echo 'no such sdk' >&2; exit 3")).rejects.toThrow(/no such sdk/);
  });
});

describe("the TypeScript SDK through its test service", () => {
  let started: Started;
  let summary: Summary;
  const lines: string[] = [];

  beforeAll(async () => {
    started = await startService("bun packages/sdk/test-service/main.ts");
    summary = await runAll({ serviceUrl: started.url, print: (line) => lines.push(line) });
  }, 170_000);

  afterAll(() => started?.stop());

  it("passes every scenario, skipping only those that need an integer accessor", () => {
    const failures = lines.filter((line) => line.startsWith("FAIL"));
    expect(failures).toEqual([]);
    expect(failed(summary)).toBe(false);
    const skipped = summary.outcomes.filter((outcome) => outcome.result === "skip");
    expect(skipped.length).toBeGreaterThanOrEqual(2);
    for (const outcome of skipped) expect(outcome.scenario.requires).toEqual(["integer-accessor"]);
  });

  it("runs every vector but the two that contract 5.5 makes bad_ruleset", () => {
    expect(summary.vectors).toEqual({ passed: 1369, skipped: 2, failures: [] });
  });

  it("prints one line per scenario and a summary", () => {
    expect(lines.filter((line) => /^(pass|skip|FAIL) /.test(line))).toHaveLength(
      summary.outcomes.length,
    );
    expect(summaryLine(summary)).toMatch(
      /^scenarios: \d+ passed, 0 failed, \d+ skipped; vectors: 1369 passed, 0 failed, 2 skipped$/,
    );
  });
});
