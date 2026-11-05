import type { AuditEvent, AuditFilters, FlagRow } from "#/api/types";

/** The shape of a flag or environment key, as the server accepts it. */
export const KEY = /^[a-z0-9][a-z0-9_-]{0,63}$/;

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

/** `yyyy-mm-dd` of an ISO 8601 time, or an empty string when there is none. */
export function day(iso: string): string {
  return iso === "" ? "" : iso.slice(0, 10);
}

/** An ISO 8601 time as `yyyy-mm-dd hh:mm:ss UTC`, fractions dropped. */
export function stamp(iso: string): string {
  return `${iso.replace("T", " ").replace(/\..*$|Z$/, "")} UTC`;
}

/** Context keys typed one per line (a key may hold a comma); blanks and repeats dropped, order kept. */
export function parseMembers(text: string): string[] {
  return [
    ...new Set(
      text
        .split("\n")
        .map((member) => member.trim())
        .filter(Boolean),
    ),
  ];
}

/** One line for an event: what happened to what, and where. */
export function describeEvent(event: AuditEvent): string {
  const where = event.environment === null ? "" : ` in ${event.environment}`;
  return `${event.action} ${event.entityType} ${event.entityId}${where}`;
}

/** The filters that are set, as search params. */
export function nonEmpty(filters: AuditFilters): {
  flag?: string;
  environment?: string;
  since?: string;
} {
  return {
    ...(filters.flag === "" ? {} : { flag: filters.flag }),
    ...(filters.environment === "" ? {} : { environment: filters.environment }),
    ...(filters.since === "" ? {} : { since: filters.since }),
  };
}

/** The number of rules in the segment's JSON text, or `null` when it is not a JSON array. */
export function ruleCount(rules: string): number | null {
  try {
    const parsed: unknown = JSON.parse(rules);
    return Array.isArray(parsed) ? parsed.length : null;
  } catch {
    return null;
  }
}
