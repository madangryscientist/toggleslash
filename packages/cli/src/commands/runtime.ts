import { apiPath } from "../api.ts";
import { ENV, PROJECT, need, printJson, str, type Command } from "../command.ts";
import { cell, details, expectObject, isObject } from "../render.ts";

const runtime: Command = {
  name: "runtime",
  usage: "--project <project> --env <env>",
  summary: "what the server is serving for an environment: ruleset tag, flag count, last change",
  options: { project: PROJECT, env: ENV },
  required: [],
  api: true,
  async run({ values }, ctx) {
    const project = need(values, "project");
    const env = need(values, "env");
    const body = await ctx
      .api(str(values, "url"))
      .get(apiPath("projects", project, "environments", env, "runtime"));
    if (printJson(values, ctx, body)) return;
    const result = expectObject(body);
    const last = result["lastChange"];
    ctx.out(
      details([
        ["etag", cell(result["etag"])],
        ["flagCount", cell(result["flagCount"])],
        ["generatedAt", cell(result["generatedAt"])],
        [
          "lastChange",
          isObject(last)
            ? [last["at"], last["actor"], last["action"], last["entityType"], last["entityId"]]
                .map(cell)
                .join("  ")
            : "-",
        ],
      ]),
    );
  },
};

export const RUNTIME: readonly Command[] = [runtime];
