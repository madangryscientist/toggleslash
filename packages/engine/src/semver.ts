/**
 * Semantic versions, to the "Semantic versions" section of `docs/evaluation.md`. A hand-written walk
 * over the string: no regular expression and no library, so it reads the same way as the C# one.
 * Nothing here converts a numeric part to a number.
 */

export interface SemanticVersion {
  major: string;
  minor: string;
  patch: string;
  /** The dot-separated prerelease identifiers; empty when there is no prerelease. */
  prerelease: readonly string[];
}

function isDigit(code: number): boolean {
  return code >= 0x30 && code <= 0x39;
}

function isIdentifierChar(code: number): boolean {
  return (
    isDigit(code) ||
    (code >= 0x41 && code <= 0x5a) ||
    (code >= 0x61 && code <= 0x7a) ||
    code === 0x2d
  );
}

function allDigits(text: string): boolean {
  for (let i = 0; i < text.length; i++) {
    if (!isDigit(text.charCodeAt(i))) return false;
  }
  return true;
}

/** `0` or digits with no leading zero. */
function isNumericPart(text: string): boolean {
  return text !== "" && allDigits(text) && (text === "0" || text.charCodeAt(0) !== 0x30);
}

/** Dot-separated, none empty, each of ASCII letters, digits and `-`. */
function identifiers(text: string): string[] | null {
  const parts = text.split(".");
  for (const part of parts) {
    if (part === "") return null;
    for (let i = 0; i < part.length; i++) {
      if (!isIdentifierChar(part.charCodeAt(i))) return null;
    }
  }
  return parts;
}

/** Text before and after the first `char`; `null` after when there is none. */
function splitAt(text: string, char: string): [string, string | null] {
  const at = text.indexOf(char);
  return at < 0 ? [text, null] : [text.slice(0, at), text.slice(at + 1)];
}

/** Prerelease identifiers: valid identifiers, and a digits-only one has no leading zero. */
function prereleaseIdentifiers(text: string): string[] | null {
  const parts = identifiers(text);
  if (parts === null) return null;
  return parts.every((part) => !allDigits(part) || isNumericPart(part)) ? parts : null;
}

/** `MAJOR.MINOR.PATCH`, or a short form (`MAJOR`, `MAJOR.MINOR`) when `short` is allowed. */
function numericParts(text: string, short: boolean): string[] | null {
  const numbers = text.split(".");
  if (numbers.length > 3 || (numbers.length < 3 && !short)) return null;
  return numbers.every(isNumericPart) ? numbers : null;
}

/** The version `text` spells, or null when it is not one. */
export function parseSemanticVersion(text: string): SemanticVersion | null {
  const [withoutBuild, build] = splitAt(text, "+");
  // Build metadata: identifiers only, and leading zeros are fine.
  if (build !== null && identifiers(build) === null) return null;
  const [core, prerelease] = splitAt(withoutBuild, "-");
  const parsedPrerelease = prerelease === null ? [] : prereleaseIdentifiers(prerelease);
  if (parsedPrerelease === null) return null;
  // The short forms carry no suffix.
  const numbers = numericParts(core, build === null && prerelease === null);
  if (numbers === null) return null;
  return {
    major: numbers[0] ?? "0",
    minor: numbers[1] ?? "0",
    patch: numbers[2] ?? "0",
    prerelease: parsedPrerelease,
  };
}

function sign(order: number): number {
  if (order < 0) return -1;
  return order > 0 ? 1 : 0;
}

/** Digit strings with no leading zero: the longer is larger, else by code unit. */
function compareNumeric(a: string, b: string): number {
  if (a.length !== b.length) return a.length < b.length ? -1 : 1;
  if (a < b) return -1;
  return a > b ? 1 : 0;
}

function compareIdentifier(a: string, b: string): number {
  const aNumeric = allDigits(a);
  const bNumeric = allDigits(b);
  if (aNumeric && bNumeric) return compareNumeric(a, b);
  if (aNumeric) return -1;
  if (bNumeric) return 1;
  if (a < b) return -1;
  return a > b ? 1 : 0;
}

function comparePrerelease(a: readonly string[], b: readonly string[]): number {
  // No prerelease is above any prerelease of the same core.
  if (a.length === 0 || b.length === 0) return sign(b.length - a.length);
  const shared = Math.min(a.length, b.length);
  for (let i = 0; i < shared; i++) {
    const order = compareIdentifier(a[i] ?? "", b[i] ?? "");
    if (order !== 0) return order;
  }
  return sign(a.length - b.length);
}

/** -1, 0 or 1 by SemVer 2.0.0 section 11; build metadata is already gone. */
export function compareSemanticVersions(a: SemanticVersion, b: SemanticVersion): number {
  const core =
    compareNumeric(a.major, b.major) ||
    compareNumeric(a.minor, b.minor) ||
    compareNumeric(a.patch, b.patch);
  return core !== 0 ? core : comparePrerelease(a.prerelease, b.prerelease);
}
