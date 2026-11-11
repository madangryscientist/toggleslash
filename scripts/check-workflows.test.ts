import { describe, expect, test } from "bun:test";
import { checkWorkflow } from "./check-workflows.ts";

const wf = (on: string, runsOn: string) =>
  `name: t\non:\n${on}\njobs:\n  j:\n    runs-on: ${runsOn}\n    steps:\n      - run: echo\n`;

describe("check-workflows", () => {
  test("accepts a hosted runner and the allowed triggers", () => {
    const on = "  push:\n  pull_request:\n  workflow_dispatch:\n  workflow_call:";
    expect(checkWorkflow("a.yml", wf(on, "ubuntu-latest"))).toEqual([]);
  });

  test("accepts a bare string trigger", () => {
    expect(
      checkWorkflow("a.yml", "name: t\non: push\njobs:\n  j:\n    runs-on: ubuntu-latest\n"),
    ).toEqual([]);
  });

  test("refuses the proxmox runner", () => {
    expect(checkWorkflow("a.yml", wf("  push:", "proxmox"))).toHaveLength(1);
  });

  test("refuses a self-hosted runner in a label list", () => {
    expect(checkWorkflow("a.yml", wf("  push:", "[self-hosted, linux]"))).toHaveLength(1);
  });

  test("refuses a runs-on expression", () => {
    expect(checkWorkflow("a.yml", wf("  push:", "${{ vars.RUNNER }}"))).toHaveLength(1);
  });

  test.each([
    "pull_request_target",
    "workflow_run",
    "issue_comment",
    "pull_request_review_comment",
  ])("refuses the %s trigger", (trigger) => {
    const failures = checkWorkflow("a.yml", wf(`  push:\n  ${trigger}:`, "ubuntu-latest"));
    expect(failures).toEqual([`a.yml: triggers on ${trigger}`]);
  });
});
