import { apiPath } from "../api.ts";
import { ENV, PROJECT, first, need, printJson, str, type Command } from "../command.ts";
import { UsageError } from "../errors.ts";
import { cell, details, expectObject, expectObjects, table, type JsonObject } from "../render.ts";

const HASH = /^[0-9a-f]{64}$/;

/** A ruleset hash as the server takes it, or a usage error before any request: the ETag's lowercase hex, no quotes. */
export function checkHash(hash: string): string {
  if (!HASH.test(hash)) {
    throw new UsageError("the hash is the ETag's 64 lowercase hex characters, without its quotes.");
  }
  return hash;
}

/** How many entries a list has: the rules and targets themselves are for `--json`. */
function count(value: unknown): string {
  return Array.isArray(value) ? String(value.length) : "-";
}

const flagRow = (f: JsonObject): string[] => [
  cell(f["key"]),
  cell(f["on"]),
  cell(f["offVariation"]),
  cell(f["fallthrough"]),
  cell(f["variations"]),
  count(f["targets"]),
  count(f["prerequisites"]),
  count(f["rules"]),
  cell(f["version"]),
];

const segmentRow = (s: JsonObject): string[] => [
  cell(s["key"]),
  count(s["included"]),
  count(s["excluded"]),
  count(s["rules"]),
];

const show: Command = {
  name: "rulesets show",
  usage: "<hash> --project <project> --env <env> [--flag <flag>]",
  summary: "what the flags were under a ruleset hash this environment served",
  options: { project: PROJECT, env: ENV, flag: { type: "string" } },
  required: ["the hash"],
  api: true,
  async run({ values, positionals }, ctx) {
    const project = need(values, "project");
    const env = need(values, "env");
    const hash = checkHash(first(positionals, "the hash"));
    const body = await ctx
      .api(str(values, "url"))
      .get(apiPath("projects", project, "environments", env, "rulesets", hash), {
        flag: str(values, "flag"),
      });
    if (printJson(values, ctx, body)) return;
    const result = expectObject(body);
    ctx.out(
      details([
        ["etag", cell(result["etag"])],
        ["firstServedAt", cell(result["firstServedAt"])],
      ]),
    );
    ctx.out("\n");
    ctx.out(
      table(
        [
          "FLAG",
          "ON",
          "OFF",
          "FALLTHROUGH",
          "VARIATIONS",
          "TARGETS",
          "PREREQUISITES",
          "RULES",
          "VERSION",
        ],
        expectObjects(result["flags"]).map(flagRow),
      ),
    );
    const segments = expectObjects(result["segments"]);
    if (segments.length > 0) {
      ctx.out("\n");
      ctx.out(table(["SEGMENT", "INCLUDED", "EXCLUDED", "RULES"], segments.map(segmentRow)));
    }
  },
};

export const RULESETS: readonly Command[] = [show];
