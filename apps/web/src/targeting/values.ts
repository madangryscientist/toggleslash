/** Clause values and context keys as a person types them. Pure. */
import type { KindMembers } from "../api/types.ts";
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

const KIND = /^[A-Za-z0-9._-]+$/;

/** A context kind as the spec allows it: letters, digits, `.`, `_` and `-`, and neither `kind` nor `multi`. */
export function isValidKind(kind: string): boolean {
  return KIND.test(kind) && kind !== "kind" && kind !== "multi";
}

/**
 * `org:acme` lines (one `kind:key` per line, split at the first colon) as keys grouped by kind, in
 * first-seen order. Blank lines are skipped; a line without a colon, with an empty key, with an invalid
 * kind or with `user` (that is what the other box is for) is refused, naming the line.
 */
export function parseKindKeys(
  text: string,
): { ok: true; entries: KindMembers[] } | { ok: false; message: string } {
  const entries: KindMembers[] = [];
  const lines = text.split("\n");
  for (let i = 0; i < lines.length; i++) {
    const line = (lines[i] ?? "").trim();
    if (line === "") continue;
    const colon = line.indexOf(":");
    if (colon < 0) return { ok: false, message: `Line ${i + 1}: write it as kind:key.` };
    const contextKind = line.slice(0, colon).trim();
    const key = line.slice(colon + 1).trim();
    if (key === "") return { ok: false, message: `Line ${i + 1}: the key is empty.` };
    if (contextKind === "user")
      return {
        ok: false,
        message: `Line ${i + 1}: user keys go in the Always included and Always excluded boxes.`,
      };
    if (!isValidKind(contextKind))
      return {
        ok: false,
        message: `Line ${i + 1}: "${contextKind}" is not a context kind (letters, digits, . _ -, and not kind or multi).`,
      };
    const entry = entries.find((e) => e.contextKind === contextKind);
    if (entry === undefined) entries.push({ contextKind, values: [key] });
    else if (!entry.values.includes(key)) entry.values.push(key);
  }
  return { ok: true, entries };
}

/** {@link parseKindKeys} the other way: one `kind:key` per line. */
export function formatKindKeys(entries: KindMembers[]): string {
  return entries.flatMap((e) => e.values.map((v) => `${e.contextKind}:${v}`)).join("\n");
}
