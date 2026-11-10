import { readFileSync } from "node:fs";
import { UsageError } from "../errors.ts";
import { isObject, type JsonObject } from "../render.ts";
import { str, type Context, type Values } from "../command.ts";

/**
 * The JSON object in `--file <path>` (`-` is stdin), read and parsed before any request.
 * Only the shape "an object" is checked here; the server checks the rest.
 */
export async function readJsonObject(values: Values, ctx: Context): Promise<JsonObject> {
  const path = str(values, "file");
  if (path === undefined || path === "") throw new UsageError("--file is required.");
  let text: string;
  if (path === "-") {
    const piped = await ctx.readStdin();
    if (piped === null) {
      throw new UsageError("--file - reads stdin, but stdin is a terminal. Pipe the JSON in.");
    }
    text = piped;
  } else {
    try {
      text = readFileSync(path, "utf8");
    } catch (error) {
      const why = error instanceof Error ? error.message : "unknown error";
      throw new UsageError(`could not read --file ${path}: ${why}`);
    }
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    throw new UsageError(`--file ${path} is not valid JSON.`);
  }
  if (!isObject(parsed)) throw new UsageError(`--file ${path} must hold a JSON object.`);
  return parsed;
}
