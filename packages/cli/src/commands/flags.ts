import { apiPath } from "../api.ts";
import {
  ENV,
  PROJECT,
  first,
  flagOn,
  need,
  printJson,
  str,
  strings,
  whole,
  type Command,
  type Values,
} from "../command.ts";
import { UsageError } from "../errors.ts";
import { cell, details, expectObject, expectObjects, table, type JsonObject } from "../render.ts";

const flagsList: Command = {
  name: "flags list",
  usage: "--project <project> [--tag <tag>] [--archived false|true|all]",
  summary: "list a project's flags (archived ones are hidden unless asked for)",
  options: { project: PROJECT, tag: { type: "string" }, archived: { type: "string" } },
  required: [],
  api: true,
  async run({ values }, ctx) {
    const project = need(values, "project");
    const archived = str(values, "archived");
    if (
      archived !== undefined &&
      archived !== "true" &&
      archived !== "false" &&
      archived !== "all"
    ) {
      throw new UsageError('--archived must be "false", "true" or "all".');
    }
    const body = await ctx
      .api(str(values, "url"))
      .get(apiPath("projects", project, "flags"), { tag: str(values, "tag"), archived });
    if (printJson(values, ctx, body)) return;
    const rows = expectObjects(body).map((f) => [
      cell(f["key"]),
      cell(f["name"]),
      cell(f["variations"]),
      Array.isArray(f["tags"]) && f["tags"].length > 0 ? f["tags"].join(",") : "-",
      f["archivedAt"] === null || f["archivedAt"] === undefined ? "active" : "archived",
      cell(f["lastEvaluated"]),
    ]);
    ctx.out(table(["KEY", "NAME", "VARIATIONS", "TAGS", "STATE", "LAST EVALUATED"], rows));
  },
};

const flagsShow: Command = {
  name: "flags show",
  usage: "<flag> --project <project> [--env <env>]",
  summary: "show a flag; with --env, also its targeting in that environment",
  options: { project: PROJECT, env: ENV },
  required: ["the flag key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const project = need(values, "project");
    const flag = first(positionals, "the flag key");
    const env = str(values, "env");
    const api = ctx.api(str(values, "url"));
    const found = expectObject(await api.get(apiPath("projects", project, "flags", flag)));
    const config =
      env === undefined
        ? undefined
        : expectObject(
            await api.get(apiPath("projects", project, "flags", flag, "environments", env)),
          );
    if (printJson(values, ctx, config === undefined ? found : { ...found, config })) return;
    const pairs: [string, string][] = [
      ["key", cell(found["key"])],
      ["name", cell(found["name"])],
      ["description", cell(found["description"])],
      ["variations", cell(found["variations"])],
      ["temporary", cell(found["temporary"])],
      ["owner", cell(found["owner"])],
      [
        "tags",
        Array.isArray(found["tags"]) && found["tags"].length > 0 ? found["tags"].join(",") : "-",
      ],
      ["created", cell(found["createdAt"])],
      ["archived", cell(found["archivedAt"])],
      ["last evaluated", cell(found["lastEvaluated"])],
    ];
    if (config !== undefined) pairs.push(...configPairs(env ?? "", config));
    ctx.out(details(pairs));
  },
};

function count(value: unknown): string {
  return Array.isArray(value) ? String(value.length) : "-";
}

function configPairs(env: string, config: JsonObject): [string, string][] {
  return [
    [`${env} on`, cell(config["on"])],
    [`${env} off variation`, cell(config["offVariation"])],
    [`${env} fallthrough`, cell(config["fallthrough"])],
    [`${env} targets`, count(config["targets"])],
    [`${env} rules`, count(config["rules"])],
    [`${env} prerequisites`, count(config["prerequisites"])],
    [`${env} version`, cell(config["version"])],
  ];
}

function parseVariations(raw: string | undefined): unknown[] {
  if (raw === undefined) return [true, false];
  let variations: unknown;
  try {
    variations = JSON.parse(raw);
  } catch {
    throw new UsageError('--variations must be a JSON array, for example \'["off","on"]\'.');
  }
  if (!Array.isArray(variations)) throw new UsageError("--variations must be a JSON array.");
  return variations;
}

function createRequest(values: Values, key: string): Record<string, unknown> {
  const request: Record<string, unknown> = {
    key,
    name: str(values, "name") ?? key,
    variations: parseVariations(str(values, "variations")),
  };
  const optional: [string, unknown][] = [
    ["description", str(values, "description")],
    ["temporary", flagOn(values, "temporary") ? true : undefined],
    ["owner", str(values, "owner")],
    ["tags", strings(values, "tag")],
    ["offVariation", whole(values, "off-variation")],
    ["fallthroughVariation", whole(values, "fallthrough-variation")],
  ];
  for (const [name, value] of optional) {
    const empty = Array.isArray(value) && value.length === 0;
    if (value !== undefined && !empty) request[name] = value;
  }
  return request;
}

const flagsCreate: Command = {
  name: "flags create",
  usage:
    "<key> --project <project> [--name <name>] [--variations <json>] [--description <text>] [--temporary] [--owner <owner>] [--tag <tag>]... [--off-variation <n>] [--fallthrough-variation <n>]",
  summary: "create a flag (variations default to [true,false]: a boolean flag)",
  options: {
    project: PROJECT,
    name: { type: "string" },
    variations: { type: "string" },
    description: { type: "string" },
    temporary: { type: "boolean" },
    owner: { type: "string" },
    tag: { type: "string", multiple: true },
    "off-variation": { type: "string" },
    "fallthrough-variation": { type: "string" },
  },
  required: ["the flag key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const project = need(values, "project");
    const key = first(positionals, "the flag key");
    const request = createRequest(values, key);

    const body = await ctx
      .api(str(values, "url"))
      .post(apiPath("projects", project, "flags"), request);
    if (printJson(values, ctx, body)) return;
    ctx.out(`created flag ${cell(expectObject(body)["key"])} in ${project}\n`);
  },
};

const flagsArchive: Command = {
  name: "flags archive",
  usage: "<flag> --project <project>",
  summary: "archive a flag (its key stays taken; archiving twice is fine)",
  options: { project: PROJECT },
  required: ["the flag key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const project = need(values, "project");
    const flag = first(positionals, "the flag key");
    const body = await ctx
      .api(str(values, "url"))
      .post(apiPath("projects", project, "flags", flag, "archive"));
    if (printJson(values, ctx, body)) return;
    ctx.out(`archived flag ${flag} in ${project}\n`);
  },
};

function toggle(on: boolean): Command {
  const word = on ? "on" : "off";
  return {
    name: `flags ${word}`,
    usage: "<flag> --project <project> --env <env>",
    summary: `turn a flag ${word} in one environment (changes nothing else about its targeting)`,
    options: { project: PROJECT, env: ENV },
    required: ["the flag key"],
    api: true,
    async run({ values, positionals }, ctx) {
      const project = need(values, "project");
      const env = need(values, "env");
      const flag = first(positionals, "the flag key");
      const body = await ctx
        .api(str(values, "url"))
        .post(apiPath("projects", project, "flags", flag, "environments", env, "toggle"), { on });
      if (printJson(values, ctx, body)) return;
      ctx.out(
        `flag ${flag} is ${word} in ${env} (version ${cell(expectObject(body)["version"])})\n`,
      );
    },
  };
}

const flagsHistory: Command = {
  name: "flags history",
  usage:
    "<flag> --project <project> [--env <env>] [--since <time>] [--limit <1-200>] [--cursor <cursor>]",
  summary: "read a flag's audit trail, newest first",
  options: {
    project: PROJECT,
    env: ENV,
    since: { type: "string" },
    limit: { type: "string" },
    cursor: { type: "string" },
  },
  required: ["the flag key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const project = need(values, "project");
    const flag = first(positionals, "the flag key");
    const limit = whole(values, "limit");
    const body = await ctx
      .api(str(values, "url"))
      .get(apiPath("projects", project, "flags", flag, "history"), {
        environment: str(values, "env"),
        since: str(values, "since"),
        limit: limit === undefined ? undefined : String(limit),
        cursor: str(values, "cursor"),
      });
    if (printJson(values, ctx, body)) return;
    const page = expectObject(body);
    const rows = expectObjects(page["events"]).map((e) => [
      cell(e["occurredAt"]),
      cell(e["actor"]),
      cell(e["action"]),
      `${cell(e["entityType"])} ${cell(e["entityId"])}`,
      cell(e["environment"]),
    ]);
    ctx.out(table(["TIME", "ACTOR", "ACTION", "ENTITY", "ENVIRONMENT"], rows));
    if (typeof page["nextCursor"] === "string") {
      ctx.out(`more: add --cursor ${page["nextCursor"]}\n`);
    }
  },
};

export const FLAGS: readonly Command[] = [
  flagsList,
  flagsShow,
  flagsCreate,
  flagsArchive,
  toggle(true),
  toggle(false),
  flagsHistory,
];
