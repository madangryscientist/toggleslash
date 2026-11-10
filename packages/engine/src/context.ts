import { bucket } from "./bucket.ts";
import { isAbsent, isKind, isRecord } from "./validate.ts";

type Attributes = Readonly<Record<string, unknown>> | null;

/** A context of one kind. No `kind` is `"user"`. */
export interface SingleContext {
  kind?: string | null;
  key: string;
  attributes?: Attributes;
}

/** One entry of a multi context. Its kind is its property name in `contexts`, so it has no `kind`. */
export interface ContextEntry {
  key: string;
  attributes?: Attributes;
}

/** Several contexts of different kinds, evaluated together. */
export interface MultiContext {
  kind: "multi";
  contexts: Readonly<Record<string, ContextEntry>>;
}

export type Context = SingleContext | MultiContext;

/** The key and attributes of one kind, after normalising. */
export interface KindContext {
  key: string;
  attributes: Attributes;
}

/** A valid context as a map from kind to that kind's key and attributes. */
export type Contexts = ReadonlyMap<string, KindContext>;

/** What a kind that is read and absent means, everywhere. */
export const DEFAULT_KIND = "user";

function readEntry(value: Record<string, unknown>): KindContext | null {
  const key = value["key"];
  const attributes = value["attributes"];
  if (typeof key !== "string" || key === "") return null;
  if (isAbsent(attributes)) return { key, attributes: null };
  return isRecord(attributes) ? { key, attributes } : null;
}

function readMulti(value: Record<string, unknown>): Contexts | null {
  const contexts = value["contexts"];
  if (!isAbsent(value["key"]) || !isAbsent(value["attributes"]) || !isRecord(contexts)) return null;
  const normalised = new Map<string, KindContext>();
  for (const [kind, raw] of Object.entries(contexts)) {
    if (!isKind(kind) || !isRecord(raw) || !isAbsent(raw["kind"])) return null;
    const entry = readEntry(raw);
    if (entry === null) return null;
    normalised.set(kind, entry);
  }
  return normalised.size > 0 ? normalised : null;
}

/** The context as a map from kind to `{ key, attributes }`, or null when it is not valid. */
export function normaliseContext(value: unknown): Contexts | null {
  if (!isRecord(value)) return null;
  const kind = value["kind"] ?? DEFAULT_KIND;
  if (kind === "multi") return readMulti(value);
  if (!isKind(kind)) return null;
  const entry = readEntry(value);
  return entry === null ? null : new Map([[kind, entry]]);
}

/**
 * The bucket for the key of `kind` (absent is `user`): the key as it is for `user`, `<kind>:<key>`
 * for any other kind, and 0 when the context does not have that kind.
 */
export function kindBucket(
  key: string,
  salt: string,
  kind: string | null | undefined,
  contexts: Contexts,
): number {
  const chosen = kind ?? DEFAULT_KIND;
  const context = contexts.get(chosen);
  if (context === undefined) return 0;
  return bucket(key, salt, chosen === DEFAULT_KIND ? context.key : `${chosen}:${context.key}`);
}
