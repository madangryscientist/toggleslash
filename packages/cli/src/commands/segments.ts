import { apiPath } from "../api.ts";
import {
  ENV,
  PROJECT,
  first,
  need,
  printJson,
  str,
  type Command,
  type Context,
  type Values,
} from "../command.ts";
import { UsageError } from "../errors.ts";
import { cell, expectObject, expectObjects, jsonText, table, type JsonObject } from "../render.ts";
import { readJsonObject } from "./json-file.ts";

const scope = { project: PROJECT, env: ENV } as const;

function segmentsPath(values: Values, ...rest: string[]): string {
  return apiPath(
    "projects",
    need(values, "project"),
    "environments",
    need(values, "env"),
    "segments",
    ...rest,
  );
}

/** The file's object without its `key`: the positional names the segment, so a different one is a mistake. */
async function readSegment(values: Values, ctx: Context, segment: string): Promise<JsonObject> {
  const { key, ...rest } = await readJsonObject(values, ctx);
  if (key !== undefined && key !== segment) {
    throw new UsageError(
      `--file has key ${cell(key)}, but the segment is ${segment}. Make them match or remove the key.`,
    );
  }
  return rest;
}

function count(value: unknown): string {
  return Array.isArray(value) ? String(value.length) : "-";
}

const segmentsList: Command = {
  name: "segments list",
  usage: "--project <project> --env <env>",
  summary: "list an environment's segments",
  options: scope,
  required: [],
  api: true,
  async run({ values }, ctx) {
    const body = await ctx.api(str(values, "url")).get(segmentsPath(values));
    if (printJson(values, ctx, body)) return;
    const rows = expectObjects(body).map((s) => [
      cell(s["key"]),
      count(s["included"]),
      count(s["excluded"]),
      count(s["rules"]),
      cell(s["version"]),
    ]);
    ctx.out(table(["KEY", "INCLUDED", "EXCLUDED", "RULES", "VERSION"], rows));
  },
};

const segmentsShow: Command = {
  name: "segments show",
  usage: "<segment> --project <project> --env <env>",
  summary: "print a segment as JSON (the input of `segments set`)",
  options: scope,
  required: ["the segment key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const segment = first(positionals, "the segment key");
    const body = await ctx.api(str(values, "url")).get(segmentsPath(values, segment));
    ctx.out(jsonText(body));
  },
};

const segmentsCreate: Command = {
  name: "segments create",
  usage: "<segment> --project <project> --env <env> [--file <path>]",
  summary: "create a segment, empty or from the JSON in a file (- is stdin)",
  options: { ...scope, file: { type: "string" } },
  required: ["the segment key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const segment = first(positionals, "the segment key");
    const fromFile =
      str(values, "file") === undefined ? {} : await readSegment(values, ctx, segment);
    const request = { included: [], excluded: [], rules: [], ...fromFile, key: segment };
    const body = await ctx.api(str(values, "url")).post(segmentsPath(values), request);
    if (printJson(values, ctx, body)) return;
    ctx.out(`created ${segment} in ${need(values, "env")}\n`);
  },
};

const segmentsSet: Command = {
  name: "segments set",
  usage: "<segment> --project <project> --env <env> --file <path>",
  summary:
    "replace a segment with the JSON in a file (- is stdin); its `version` must be the one `segments show` printed",
  options: { ...scope, file: { type: "string" } },
  required: ["the segment key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const segment = first(positionals, "the segment key");
    const request = await readSegment(values, ctx, segment);
    const body = await ctx.api(str(values, "url")).put(segmentsPath(values, segment), request);
    if (printJson(values, ctx, body)) return;
    ctx.out(
      `saved ${segment} in ${need(values, "env")} at version ${cell(expectObject(body)["version"])}\n`,
    );
  },
};

const segmentsDelete: Command = {
  name: "segments delete",
  usage: "<segment> --project <project> --env <env>",
  summary: "delete a segment (refused while a flag still uses it)",
  options: scope,
  required: ["the segment key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const segment = first(positionals, "the segment key");
    await ctx.api(str(values, "url")).delete(segmentsPath(values, segment));
    if (printJson(values, ctx, { key: segment, deleted: true })) return;
    ctx.out(`deleted ${segment} from ${need(values, "env")}\n`);
  },
};

export const SEGMENTS: readonly Command[] = [
  segmentsList,
  segmentsShow,
  segmentsCreate,
  segmentsSet,
  segmentsDelete,
];
