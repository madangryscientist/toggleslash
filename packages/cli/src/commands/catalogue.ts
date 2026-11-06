import { apiPath } from "../api.ts";
import { UsageError } from "../errors.ts";
import { PROJECT, first, flagOn, need, printJson, str, type Command } from "../command.ts";
import { cell, expectObject, expectObjects, table } from "../render.ts";

const projectsList: Command = {
  name: "projects list",
  usage: "",
  summary: "list projects",
  options: {},
  required: [],
  api: true,
  async run({ values }, ctx) {
    const body = await ctx.api(str(values, "url")).get(apiPath("projects"));
    if (printJson(values, ctx, body)) return;
    const rows = expectObjects(body).map((p) => [
      cell(p["key"]),
      cell(p["name"]),
      cell(p["createdAt"]),
    ]);
    ctx.out(table(["KEY", "NAME", "CREATED"], rows));
  },
};

const projectsCreate: Command = {
  name: "projects create",
  usage: "<key> [--name <name>]",
  summary: "create a project (the name defaults to the key)",
  options: { name: { type: "string" } },
  required: ["the project key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const key = first(positionals, "the project key");
    const body = await ctx.api(str(values, "url")).post(apiPath("projects"), {
      key,
      name: str(values, "name") ?? key,
    });
    if (printJson(values, ctx, body)) return;
    ctx.out(`created project ${cell(expectObject(body)["key"])}\n`);
  },
};

const envsList: Command = {
  name: "envs list",
  usage: "--project <project>",
  summary: "list a project's environments",
  options: { project: PROJECT },
  required: [],
  api: true,
  async run({ values }, ctx) {
    const project = need(values, "project");
    const body = await ctx
      .api(str(values, "url"))
      .get(apiPath("projects", project, "environments"));
    if (printJson(values, ctx, body)) return;
    const rows = expectObjects(body).map((e) => [
      cell(e["key"]),
      cell(e["name"]),
      cell(e["createdAt"]),
    ]);
    ctx.out(table(["KEY", "NAME", "CREATED"], rows));
  },
};

const envsCreate: Command = {
  name: "envs create",
  usage: "<key> --project <project> [--name <name>]",
  summary: "create an environment (the name defaults to the key)",
  options: { project: PROJECT, name: { type: "string" } },
  required: ["the environment key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const project = need(values, "project");
    const key = first(positionals, "the environment key");
    const body = await ctx
      .api(str(values, "url"))
      .post(apiPath("projects", project, "environments"), {
        key,
        name: str(values, "name") ?? key,
      });
    if (printJson(values, ctx, body)) return;
    ctx.out(`created environment ${cell(expectObject(body)["key"])} in ${project}\n`);
  },
};

const envsRename: Command = {
  name: "envs rename",
  usage: "<key> --project <project> --name <name>",
  summary: "rename an environment (its key never changes)",
  options: { project: PROJECT, name: { type: "string" } },
  required: ["the environment key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const project = need(values, "project");
    const key = first(positionals, "the environment key");
    const name = need(values, "name");
    const body = await ctx
      .api(str(values, "url"))
      .patch(apiPath("projects", project, "environments", key), { name });
    if (printJson(values, ctx, body)) return;
    const renamed = expectObject(body);
    ctx.out(
      `renamed environment ${cell(renamed["key"])} in ${project} to ${cell(renamed["name"])}\n`,
    );
  },
};

const envsDelete: Command = {
  name: "envs delete",
  usage: "<key> --project <project> --yes",
  summary:
    "delete an environment and its flag settings, segments, usage and kept rulesets (--yes is required; refused while an SDK key is not revoked)",
  options: { project: PROJECT, yes: { type: "boolean" } },
  required: ["the environment key"],
  api: true,
  async run({ values, positionals }, ctx) {
    const project = need(values, "project");
    const key = first(positionals, "the environment key");
    // No prompt, because the CLI is scripted: the flag is the confirmation, and without it nothing is sent.
    if (!flagOn(values, "yes")) {
      throw new UsageError(
        `deleting an environment cannot be undone: pass --yes to delete ${key}.`,
      );
    }
    await ctx.api(str(values, "url")).delete(apiPath("projects", project, "environments", key));
    if (printJson(values, ctx, { key, deleted: true })) return;
    ctx.out(`deleted environment ${key} in ${project}\n`);
  },
};

export const CATALOGUE: readonly Command[] = [
  projectsList,
  projectsCreate,
  envsList,
  envsCreate,
  envsRename,
  envsDelete,
];
