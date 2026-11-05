import { apiPath } from "../api.ts";
import { ENV, PROJECT, first, need, printJson, str, type Command } from "../command.ts";
import { UsageError } from "../errors.ts";
import { cell, details, expectObject, expectObjects, table } from "../render.ts";

const keysCreate: Command = {
  name: "keys create",
  usage: "--project <project> --env <env> --kind server|client --name <name>",
  summary: "create an SDK key; the plain key is printed once and cannot be read again",
  options: { project: PROJECT, env: ENV, kind: { type: "string" }, name: { type: "string" } },
  required: [],
  api: true,
  async run({ values }, ctx) {
    const project = need(values, "project");
    const env = need(values, "env");
    const kind = need(values, "kind");
    if (kind !== "server" && kind !== "client")
      throw new UsageError('--kind must be "server" or "client".');
    const name = need(values, "name");
    const body = await ctx
      .api(str(values, "url"))
      .post(apiPath("projects", project, "environments", env, "keys"), { kind, name });
    if (printJson(values, ctx, body)) return;
    const created = expectObject(body);
    ctx.out(
      details([
        ["id", cell(created["id"])],
        ["kind", cell(created["kind"])],
        ["name", cell(created["name"])],
        ["key", cell(created["key"])],
      ]),
    );
    ctx.err(
      "featureslash: store the key now; the server keeps only a hash and will not show it again.\n",
    );
  },
};

const keysList: Command = {
  name: "keys list",
  usage: "--project <project> --env <env>",
  summary: "list an environment's SDK keys (prefixes only, never the key)",
  options: { project: PROJECT, env: ENV },
  required: [],
  api: true,
  async run({ values }, ctx) {
    const project = need(values, "project");
    const env = need(values, "env");
    const body = await ctx
      .api(str(values, "url"))
      .get(apiPath("projects", project, "environments", env, "keys"));
    if (printJson(values, ctx, body)) return;
    const rows = expectObjects(body).map((k) => [
      cell(k["id"]),
      cell(k["kind"]),
      cell(k["name"]),
      cell(k["prefix"]),
      cell(k["createdAt"]),
      cell(k["revokedAt"]),
    ]);
    ctx.out(table(["ID", "KIND", "NAME", "PREFIX", "CREATED", "REVOKED"], rows));
  },
};

const keysRevoke: Command = {
  name: "keys revoke",
  usage: "<id> --project <project> --env <env>",
  summary: "revoke an SDK key by its id (revoking twice is fine)",
  options: { project: PROJECT, env: ENV },
  required: ["the key id"],
  api: true,
  async run({ values, positionals }, ctx) {
    const project = need(values, "project");
    const env = need(values, "env");
    const id = first(positionals, "the key id");
    if (!/^\d+$/.test(id)) throw new UsageError("the key id is the number `keys list` shows.");
    await ctx
      .api(str(values, "url"))
      .delete(apiPath("projects", project, "environments", env, "keys", id));
    if (printJson(values, ctx, { id: Number(id), revoked: true })) return;
    ctx.out(`revoked key ${id}\n`);
  },
};

export const KEYS: readonly Command[] = [keysCreate, keysList, keysRevoke];
