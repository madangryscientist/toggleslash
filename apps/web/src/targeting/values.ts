/** Clause values and context keys as a person types them. Pure. */
import type { ClauseValue } from "./model.ts";

const NEEDS_QUOTES = /^(true|false)$|^-?\d+(\.\d+)?([eE][+-]?\d+)?$|^\s|\s$|[",]|^$/;

/** One value as typed: a string that would read back as a number, a boolean or a list is quoted. */
export function formatValue(value: ClauseValue): string {
  if (typeof value !== "string") return String(value);
  return NEEDS_QUOTES.test(value) ? JSON.stringify(value) : value;
}

export function formatValues(values: ClauseValue[]): string {
  return values.map(formatValue).join(", ");
}

const NUMBER = /^-?\d+(\.\d+)?([eE][+-]?\d+)?$/;

/** A `"quoted"` token as the string it holds; `null` for a token that is not one. */
function quoted(token: string): string | null {
  if (!token.startsWith('"')) return null;
  try {
    const parsed: unknown = JSON.parse(token);
    return typeof parsed === "string" ? parsed : null;
  } catch {
    return null;
  }
}

function bare(token: string): ClauseValue {
  if (token === "true" || token === "false") return token === "true";
  if (NUMBER.test(token) && Number.isFinite(Number(token))) return Number(token);
  return token;
}

/**
 * `a, 12, true, "42", "x, y"` as `["a", 12, true, "42", "x, y"]`. A bare `true`/`false` is a
 * boolean and a bare number a number; quote a value ("42") to keep it a string.
 */
export function parseValues(text: string, typed = true): ClauseValue[] {
  const tokens = text.match(/\s*"(?:[^"\\]|\\.)*"\s*|[^,]+/g) ?? [];
  return tokens
    .map((t) => t.trim())
    .filter((t) => t !== "")
    .map((t) => quoted(t) ?? (typed ? bare(t) : t));
}

/** The same tokens as {@link parseValues}, all kept as strings (segment keys are never numbers or booleans). */
export function parseStrings(text: string): string[] {
  return parseValues(text, false).map(String);
}

export function parseKeys(text: string): string[] {
  return text
    .split(/[\n,]/)
    .map((k) => k.trim())
    .filter((k) => k !== "");
}
