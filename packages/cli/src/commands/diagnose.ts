import { readFileSync } from "node:fs";
import { apiPath } from "../api.ts";
import {
  ENV,
  PROJECT,
  first,
  need,
  printJson,
  str,
  type Command,
  type Values,
} from "../command.ts";
import { FailureError, UsageError } from "../errors.ts";
import { cell, details, expectObject, isObject } from "../render.ts";

/** The context to evaluate, from exactly one of `--context` and `--context-file`. Reads and parses before any request. */
function readContext(values: Values): unknown {
  const inline = str(values, "context");
  const path = str(values, "context-file");
  if ((inline === undefined) === (path === undefined)) {
    throw new UsageError("give exactly one of --context <json> and --context-file <path>.");
  }
  let text = inline ?? "";
  if (path !== undefined) {
    try {
      text = readFileSync(path, "utf8");
    } catch (error) {
      const why = error instanceof Error ? error.message : "unknown error";
      throw new UsageError(`could not read --context-file ${path}: ${why}`);
    }
  }
  try {
    return JSON.parse(text);
  } catch {
    throw new UsageError(
      `${inline === undefined ? `--context-file ${path}` : "--context"} is not valid JSON.`,
    );
  }
}

const diagnose: Command = {
  name: "diagnose",
  usage: "<flag> --project <project> --env <env> (--context '<json>' | --context-file <path>)",
  summary: "evaluate a flag for a context on the server, and say why (counts no usage)",
  options: {
    project: PROJECT,
    env: ENV,
    context: { type: "string" },
    "context-file": { type: "string" },
  },
  required: ["the flag key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const project = need(values, "project");
    const env = need(values, "env");
    const flag = first(positionals, "the flag key");
    const context = readContext(values);
    const body = await ctx
      .api(str(values, "url"))
      .post(apiPath("projects", project, "environments", env, "evaluate"), {
        flagKey: flag,
        context,
      });
    if (printJson(values, ctx, body)) return;
    const flags = expectObject(expectObject(body)["flags"]);
    const result = flags[flag];
    if (!isObject(result)) {
      throw new FailureError(`the server answered without a result for ${flag}.`);
    }
    const reason = isObject(result["reason"]) ? result["reason"] : {};
    const lines: [string, string][] = [
      ["value", cell(result["value"])],
      ["variationIndex", cell(result["variationIndex"])],
      ["reason", cell(reason["kind"])],
    ];
    if (reason["ruleIndex"] !== undefined) lines.push(["ruleIndex", cell(reason["ruleIndex"])]);
    if (reason["flagKey"] !== undefined) lines.push(["flagKey", cell(reason["flagKey"])]);
    if (reason["code"] !== undefined) lines.push(["code", cell(reason["code"])]);
    ctx.out(details(lines));
  },
};

export const DIAGNOSE: readonly Command[] = [diagnose];
