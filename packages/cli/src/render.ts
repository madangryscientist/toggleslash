import { FailureError } from "./errors.ts";
import type { Json } from "./api.ts";

export type JsonObject = Record<string, unknown>;

export function isObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** An object the server was meant to send. Anything else is a server failure, not a crash. */
export function expectObject(value: Json): JsonObject {
  if (!isObject(value))
    throw new FailureError("the server answered with something other than the expected object.");
  return value;
}

/** A list of objects the server was meant to send. */
export function expectObjects(value: Json): JsonObject[] {
  if (!Array.isArray(value) || !value.every(isObject)) {
    throw new FailureError("the server answered with something other than the expected list.");
  }
  return value;
}

/** One value as a cell: text as it is, `null` as `-`, anything else as compact JSON. */
export function cell(value: unknown): string {
  if (value === undefined || value === null) return "-";
  if (typeof value === "string") return value === "" ? "-" : value;
  return JSON.stringify(value);
}

/** Left-aligned columns separated by two spaces, with a header row; no trailing spaces. */
export function table(headers: readonly string[], rows: readonly (readonly string[])[]): string {
  const widths = headers.map((header, column) =>
    Math.max(header.length, ...rows.map((row) => (row[column] ?? "").length)),
  );
  const line = (cells: readonly string[]): string =>
    cells
      .map((text, column) => text.padEnd(widths[column] ?? 0))
      .join("  ")
      .trimEnd();
  return `${[line(headers), ...rows.map(line)].join("\n")}\n`;
}

/** `name:  value` lines with the values lined up. */
export function details(pairs: readonly (readonly [string, string])[]): string {
  const width = Math.max(...pairs.map(([name]) => name.length + 1));
  return `${pairs.map(([name, value]) => `${`${name}:`.padEnd(width)}  ${value}`).join("\n")}\n`;
}

/** Pretty JSON for `--json`: exactly what the server sent, so a script sees the API's own shapes. */
export function jsonText(value: Json): string {
  return `${JSON.stringify(value, null, 2)}\n`;
}
