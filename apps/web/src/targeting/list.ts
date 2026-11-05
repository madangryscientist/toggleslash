/** Small immutable list edits for the editor. */

export function replaceAt<T>(items: readonly T[], index: number, item: T): T[] {
  return items.map((x, i) => (i === index ? item : x));
}

export function removeAt<T>(items: readonly T[], index: number): T[] {
  return items.filter((_, i) => i !== index);
}

export function moveBy<T>(items: readonly T[], index: number, by: -1 | 1): T[] {
  const to = index + by;
  const item = items[index];
  if (item === undefined || to < 0 || to >= items.length) return [...items];
  const out = removeAt(items, index);
  out.splice(to, 0, item);
  return out;
}
