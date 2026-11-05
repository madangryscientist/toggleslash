import type { ParseArgsOptionsConfig } from "node:util";
import type { Api, Json } from "./api.ts";
import type { ConfigEnv } from "./config.ts";
import { UsageError } from "./errors.ts";
import { jsonText } from "./render.ts";

export interface Context {
  where: ConfigEnv;
  /** The admin API for the resolved URL and token. Throws a usage error when either is missing. */
  api: (urlFlag: string | undefined) => Api;
  /** Stdin as text, or `null` when stdin is a terminal. */
  readStdin: () => Promise<string | null>;
  out: (text: string) => void;
  err: (text: string) => void;
  /** Tells the output about a value that must never be printed. */
  hide: (secret: string) => void;
}

export type Values = Record<string, string | boolean | (string | boolean)[] | undefined>;

export interface Parsed {
  values: Values;
  positionals: string[];
}

export interface Command {
  /** Group and action, as typed: `flags create`. */
  name: string;
  /** What follows the name in `--help`: positionals and options. */
  usage: string;
  summary: string;
  /** Options besides `--json`, `--url` and `--help`, which every command takes. */
  options: ParseArgsOptionsConfig;
  /** The positional arguments, in order, by what each is. */
  required: string[];
  /** True for a command that calls the API (it then takes `--url`). */
  api: boolean;
  run(input: Parsed, ctx: Context): Promise<void>;
}

export function str(values: Values, name: string): string | undefined {
  const value = values[name];
  return typeof value === "string" ? value : undefined;
}

export function flagOn(values: Values, name: string): boolean {
  return values[name] === true;
}

export function strings(values: Values, name: string): string[] {
  const value = values[name];
  return Array.isArray(value) ? value.filter((item) => typeof item === "string") : [];
}

export function need(values: Values, name: string): string {
  const value = str(values, name);
  if (value === undefined || value.trim() === "") {
    throw new UsageError(`--${name} is required.`);
  }
  return value;
}

export function whole(values: Values, name: string): number | undefined {
  const raw = str(values, name);
  if (raw === undefined) return undefined;
  if (!/^-?\d+$/.test(raw)) throw new UsageError(`--${name} must be a whole number.`);
  return Number(raw);
}

export function first(positionals: string[], what: string): string {
  const [value] = positionals;
  if (value === undefined) throw new UsageError(`${what} is required.`);
  return value;
}

/** Prints `value` as JSON when `--json` is on, and returns whether it did. */
export function printJson(values: Values, ctx: Context, value: Json): boolean {
  if (!flagOn(values, "json")) return false;
  ctx.out(jsonText(value));
  return true;
}

export const PROJECT = { type: "string", short: "p" } as const;
export const ENV = { type: "string", short: "e" } as const;
