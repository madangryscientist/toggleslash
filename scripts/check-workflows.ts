#!/usr/bin/env bun
// Keeps CI safe for a public repository.
//
// Usage: bun scripts/check-workflows.ts <file.yml>...
//
// A public repository runs a fork's pull request on our workflows, so:
//   - no job may run on a self-hosted runner: its `runs-on` must not name `proxmox` or `self-hosted`,
//     and must not be an expression (an expression could resolve to either), and
//   - no workflow may trigger on anything but push, pull_request, workflow_dispatch or workflow_call
//     (never pull_request_target, workflow_run, issue_comment or any other trigger a fork can drive
//     with more than a read-only token).
// Exits 1 if any rule fails.
import { basename } from "node:path";

const ALLOWED_TRIGGERS = new Set(["push", "pull_request", "workflow_dispatch", "workflow_call"]);

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function triggers(on: unknown): string[] {
  if (typeof on === "string") return [on];
  if (Array.isArray(on)) return on.map(String);
  if (isRecord(on)) return Object.keys(on);
  return [];
}

function forbiddenRunner(runsOn: unknown): boolean {
  const text = JSON.stringify(runsOn ?? "");
  return text.includes("proxmox") || text.includes("self-hosted") || text.includes("${{");
}

export function checkWorkflow(name: string, text: string): string[] {
  const wf: unknown = Bun.YAML.parse(text);
  const failures: string[] = [];
  if (!isRecord(wf)) return failures;
  // YAML 1.1 parsers read a bare `on:` key as boolean true; accept either spelling.
  const bad = triggers(wf.on ?? wf.true).filter((t) => !ALLOWED_TRIGGERS.has(t));
  if (bad.length) failures.push(`${name}: triggers on ${bad.join(", ")}`);
  const jobs = isRecord(wf.jobs) ? wf.jobs : {};
  for (const [id, job] of Object.entries(jobs)) {
    if (isRecord(job) && forbiddenRunner(job["runs-on"]))
      failures.push(`${name}: job ${id} runs on a self-hosted runner or an expression`);
  }
  return failures;
}

if (import.meta.main) {
  const files = Bun.argv.slice(2);
  const failures: string[] = [];
  for (const f of files) {
    failures.push(...checkWorkflow(basename(f), await Bun.file(f).text()));
  }
  for (const line of failures) console.log(`FAIL ${line}`);
  console.log(`${files.length} workflow(s) read, ${failures.length} failure(s)`);
  process.exit(failures.length ? 1 : 0);
}
