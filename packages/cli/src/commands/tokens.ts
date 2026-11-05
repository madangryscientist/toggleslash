import { apiPath } from "../api.ts";
import { PROJECT, first, need, printJson, str, whole, type Command } from "../command.ts";
import { UsageError } from "../errors.ts";
import { cell, details, expectObject, expectObjects, table } from "../render.ts";

const tokensCreate: Command = {
  name: "tokens create",
  usage: "--project <project> --name <name> --permission read|write --expires-in-days <1-365>",
  summary: "create a scoped API token; the plain token is printed once and cannot be read again",
  options: {
    project: PROJECT,
    name: { type: "string" },
    permission: { type: "string" },
    "expires-in-days": { type: "string" },
  },
  required: [],
  api: true,
  async run({ values }, ctx) {
    const project = need(values, "project");
    const name = need(values, "name");
    const permission = need(values, "permission");
    if (permission !== "read" && permission !== "write")
      throw new UsageError('--permission must be "read" or "write".');
    const expiresInDays = whole(values, "expires-in-days");
    if (expiresInDays === undefined) throw new UsageError("--expires-in-days is required.");
    const body = await ctx
      .api(str(values, "url"))
      .post(apiPath("projects", project, "tokens"), { name, permission, expiresInDays });
    if (printJson(values, ctx, body)) return;
    const created = expectObject(body);
    ctx.out(
      details([
        ["id", cell(created["id"])],
        ["name", cell(created["name"])],
        ["permission", cell(created["permission"])],
        ["expires", cell(created["expiresAt"])],
        ["token", cell(created["token"])],
      ]),
    );
    ctx.err(
      "featureslash: store the token now; the server keeps only a hash and will not show it again.\n",
    );
  },
};

const tokensList: Command = {
  name: "tokens list",
  usage: "--project <project>",
  summary: "list a project's scoped API tokens, expired and revoked too (prefixes only)",
  options: { project: PROJECT },
  required: [],
  api: true,
  async run({ values }, ctx) {
    const project = need(values, "project");
    const body = await ctx.api(str(values, "url")).get(apiPath("projects", project, "tokens"));
    if (printJson(values, ctx, body)) return;
    const rows = expectObjects(body).map((t) => [
      cell(t["id"]),
      cell(t["name"]),
      cell(t["permission"]),
      cell(t["prefix"]),
      cell(t["createdAt"]),
      cell(t["expiresAt"]),
      cell(t["revokedAt"]),
    ]);
    ctx.out(table(["ID", "NAME", "PERMISSION", "PREFIX", "CREATED", "EXPIRES", "REVOKED"], rows));
  },
};

const tokensRevoke: Command = {
  name: "tokens revoke",
  usage: "<id> --project <project>",
  summary: "revoke a scoped API token by its id (revoking twice is fine)",
  options: { project: PROJECT },
  required: ["the token id"],
  api: true,
  async run({ values, positionals }, ctx) {
    const project = need(values, "project");
    const id = first(positionals, "the token id");
    if (!/^\d+$/.test(id)) throw new UsageError("the token id is the number `tokens list` shows.");
    await ctx.api(str(values, "url")).delete(apiPath("projects", project, "tokens", id));
    if (printJson(values, ctx, { id: Number(id), revoked: true })) return;
    ctx.out(`revoked token ${id}\n`);
  },
};

export const TOKENS: readonly Command[] = [tokensCreate, tokensList, tokensRevoke];
