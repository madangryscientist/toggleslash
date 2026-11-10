import type { Lifecycle } from "./types.ts";

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

/** The flag lifecycle stage in `value`, or `null` for anything else. */
export function lifecycleOf(value: unknown): Lifecycle | null {
  return value === "new" || value === "active" || value === "inactive" || value === "archived"
    ? value
    : null;
}

export interface Selection {
  project?: string | undefined;
  env?: string | undefined;
}

/** The project and environment a server function was asked about, when it was asked. */
export function selection(data: unknown): Selection {
  const d = recordOf(data);
  return {
    project: typeof d["project"] === "string" ? d["project"] : undefined,
    env: typeof d["env"] === "string" ? d["env"] : undefined,
  };
}
