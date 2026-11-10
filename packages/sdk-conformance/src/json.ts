/** Reading the scenarios and the service's answers, which are JSON the harness does not trust. */

export type Rec = Record<string, unknown>;

export function isRecord(value: unknown): value is Rec {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function record(value: unknown, what: string): Rec {
  if (!isRecord(value)) throw new Error(`${what} is not an object: ${show(value)}`);
  return value;
}

export function list(value: unknown, what: string): unknown[] {
  if (!Array.isArray(value)) throw new Error(`${what} is not an array: ${show(value)}`);
  return value;
}

export function str(rec: Rec, name: string): string {
  const value = rec[name];
  if (typeof value !== "string") throw new Error(`"${name}" is not a string: ${show(value)}`);
  return value;
}

export function num(rec: Rec, name: string): number {
  const value = rec[name];
  if (typeof value !== "number") throw new Error(`"${name}" is not a number: ${show(value)}`);
  return value;
}

export function optNum(rec: Rec, name: string): number | undefined {
  return rec[name] === undefined ? undefined : num(rec, name);
}

export function optStr(rec: Rec, name: string): string | undefined {
  return rec[name] === undefined ? undefined : str(rec, name);
}

/** JSON equality: same kinds, same members, arrays in order. */
export function same(a: unknown, b: unknown): boolean {
  return Bun.deepEquals(a, b, true);
}

/** A value for a message, cut short so one huge body does not drown the report. */
export function show(value: unknown, max = 300): string {
  const text = value === undefined ? "undefined" : JSON.stringify(value);
  return text.length > max ? `${text.slice(0, max)}...` : text;
}

/** Each field `expected` names must equal the same field of `actual`; other fields are not checked. */
export function partialMismatch(actual: Rec, expected: Rec): string | null {
  for (const [name, want] of Object.entries(expected)) {
    if (!same(actual[name], want))
      return `${name} was ${show(actual[name])}, expected ${show(want)}`;
  }
  return null;
}

/** A count expectation: a number means exactly that many, `{ min, max }` a range (either side optional). */
export function inRange(actual: number, expected: unknown, what: string): string | null {
  if (typeof expected === "number")
    return actual === expected ? null : `${what} was ${actual}, expected ${expected}`;
  const range = record(expected, what);
  const min = optNum(range, "min");
  const max = optNum(range, "max");
  if (min !== undefined && actual < min) return `${what} was ${actual}, expected at least ${min}`;
  if (max !== undefined && actual > max) return `${what} was ${actual}, expected at most ${max}`;
  return null;
}

const POSITIVE = "\u0000+inf";
const NEGATIVE = "\u0000-inf";

/**
 * JSON text for a value that came from JSON text. `1e999` in a fixture parses to Infinity, which
 * `JSON.stringify` would send as `null`; this sends it as `1e999` again, so the SDK reads what the vector says.
 */
export function toJson(value: unknown): string {
  const text = JSON.stringify(value, (_name, item: unknown) => {
    if (item === Infinity) return POSITIVE;
    if (item === -Infinity) return NEGATIVE;
    return item;
  });
  return text
    .replaceAll(JSON.stringify(POSITIVE), "1e999")
    .replaceAll(JSON.stringify(NEGATIVE), "-1e999");
}
