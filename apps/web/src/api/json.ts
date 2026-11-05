/** Narrowing for values that arrive as JSON, so nothing needs a type assertion. */
export type Json = Record<string, unknown>;

export function isRecord(value: unknown): value is Json {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** `value` when it is a string, else `fallback`. */
export function str(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

/** The strings in `value` when it is an array, else none. */
export function strings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item) => typeof item === "string") : [];
}

/** `value` when it is an object, else an empty one, so a handler can read fields off untrusted input. */
export function recordOf(value: unknown): Json {
  return isRecord(value) ? value : {};
}
