import type { FlagRow } from "#/api/types";

/** A flag matches when every word of the search is in its key, name or a tag. */
export function matches(flag: FlagRow, query: string): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const haystack = [flag.key, flag.name, ...flag.tags].join(" ").toLowerCase();
  return words.every((word) => haystack.includes(word));
}

/** Tags are typed as `a, b`; the server wants each lowercase. */
export function parseTags(text: string): string[] {
  return [
    ...new Set(
      text
        .toLowerCase()
        .split(/[\s,]+/)
        .filter(Boolean),
    ),
  ];
}
