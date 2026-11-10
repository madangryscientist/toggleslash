import { apiPath } from "../api.ts";
import { ENV, PROJECT, first, need, printJson, str, type Command } from "../command.ts";
import { cell, expectObject, jsonText } from "../render.ts";
import { readJsonObject } from "./json-file.ts";

const targetingShow: Command = {
  name: "targeting show",
  usage: "<flag> --project <project> --env <env>",
  summary: "print a flag's targeting in one environment as JSON (the input of `targeting set`)",
  options: { project: PROJECT, env: ENV },
  required: ["the flag key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const project = need(values, "project");
    const env = need(values, "env");
    const flag = first(positionals, "the flag key");
    const body = await ctx
      .api(str(values, "url"))
      .get(apiPath("projects", project, "flags", flag, "environments", env));
    // Always JSON, with or without --json: the output is the input of `targeting set`.
    ctx.out(jsonText(body));
  },
};

const targetingSet: Command = {
  name: "targeting set",
  usage: "<flag> --project <project> --env <env> --file <path>",
  summary:
    "replace a flag's targeting in one environment with the JSON in a file (- is stdin); its `version` must be the one `targeting show` printed",
  options: { project: PROJECT, env: ENV, file: { type: "string" } },
  required: ["the flag key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const project = need(values, "project");
    const env = need(values, "env");
    const flag = first(positionals, "the flag key");
    const request = await readJsonObject(values, ctx);
    const body = await ctx
      .api(str(values, "url"))
      .put(apiPath("projects", project, "flags", flag, "environments", env), request);
    if (printJson(values, ctx, body)) return;
    ctx.out(`saved ${flag} in ${env} at version ${cell(expectObject(body)["version"])}\n`);
  },
};

export const TARGETING: readonly Command[] = [targetingShow, targetingSet];
