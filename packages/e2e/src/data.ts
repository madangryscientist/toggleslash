import type { Context } from "@hackcove/toggleslash-sdk";

export function record(value: unknown): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`expected an object, got ${JSON.stringify(value)}`);
  }
  return Object.fromEntries(Object.entries(value));
}

export function text(value: unknown): string {
  if (typeof value !== "string") throw new Error(`expected a string, got ${JSON.stringify(value)}`);
  return value;
}

/** Contexts that exercise every clause the flags below use, plus keys whose bytes differ across encodings. */
export function contexts(): Context[] {
  const plans = ["free", "pro", "team"];
  const countries = ["ZA", "US", "NL"];
  const list: Context[] = Array.from({ length: 60 }, (_, i) => ({
    key: `user-${i}`,
    attributes: {
      plan: plans[i % 3],
      country: countries[i % 3 === 0 ? (i >> 1) % 3 : i % 3],
      age: 18 + (i % 40),
    },
  }));
  list.push(
    { key: "no-attributes" },
    { key: "ñandú-é", attributes: { plan: "pro", age: 40 } },
    { key: "日本語-🎌", attributes: { plan: "team", country: "NL", age: 30 } },
    { key: "user-3", attributes: { plan: "free", country: "ZA" } },
  );
  return list;
}
