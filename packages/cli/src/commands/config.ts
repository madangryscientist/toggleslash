import { configPath, normaliseUrl, readConfig, writeConfig } from "../config.ts";
import { first, type Command } from "../command.ts";
import { UsageError } from "../errors.ts";

const configSetUrl: Command = {
  name: "config set-url",
  usage: "<url>",
  summary: "save the server URL in the config file",
  options: {},
  required: ["the server URL"],
  api: false,
  async run({ positionals }, ctx) {
    const url = normaliseUrl(first(positionals, "the server URL"));
    const path = configPath(ctx.where);
    writeConfig(path, { ...readConfig(path), url });
    ctx.out(`saved the server URL (${url}) in ${path}\n`);
  },
};

const configSetToken: Command = {
  name: "config set-token",
  usage: "",
  summary: "save the admin token (read from stdin, never an argument) in the config file, mode 600",
  options: {},
  required: [],
  api: false,
  async run(_input, ctx) {
    const piped = await ctx.readStdin();
    if (piped === null) {
      throw new UsageError(
        "pipe the token in (`featureslash config set-token < token-file`); it is never an argument, so it stays out of your shell history.",
      );
    }
    const token = piped.trim();
    if (token === "") throw new UsageError("the admin token is empty.");
    ctx.hide(token);
    const path = configPath(ctx.where);
    writeConfig(path, { ...readConfig(path), token });
    ctx.out(`saved the admin token in ${path} (mode 600)\n`);
  },
};

export const CONFIG: readonly Command[] = [configSetUrl, configSetToken];
