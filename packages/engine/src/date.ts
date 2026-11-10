/**
 * Instants, to the "Dates" section of `docs/evaluation.md`. A hand-written walk over the string: no
 * regular expression, no `Date.parse` and no library, so it reads the same way as the C# one. The
 * result is Unix milliseconds as a binary64 number, or null when the text is not an instant.
 */

function isDigit(code: number): boolean {
  return code >= 0x30 && code <= 0x39;
}

/** `length` ASCII digits at `start`, as a number; null when any is not a digit. */
function digits(text: string, start: number, length: number): number | null {
  let value = 0;
  for (let i = start; i < start + length; i++) {
    const code = text.charCodeAt(i);
    if (!isDigit(code)) return null;
    value = value * 10 + (code - 0x30);
  }
  return value;
}

function isLeapYear(year: number): boolean {
  return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
}

function daysInMonth(year: number, month: number): number {
  if (month === 2) return isLeapYear(year) ? 29 : 28;
  return month === 4 || month === 6 || month === 9 || month === 11 ? 30 : 31;
}

/**
 * Days since 1970-01-01 in the proleptic Gregorian calendar ("days from civil"). For years 1 to 9999
 * every value before the last subtraction is non-negative, so floor and truncating division agree.
 */
function daysFromCivil(year: number, month: number, day: number): number {
  const y = month <= 2 ? year - 1 : year;
  const era = Math.floor(y / 400);
  const yearOfEra = y - era * 400;
  const monthIndex = month > 2 ? month - 3 : month + 9;
  const dayOfYear = Math.floor((153 * monthIndex + 2) / 5) + day - 1;
  const dayOfEra =
    yearOfEra * 365 + Math.floor(yearOfEra / 4) - Math.floor(yearOfEra / 100) + dayOfYear;
  return era * 146097 + dayOfEra - 719468;
}

/** The days since 1970-01-01 of the date `YYYY-MM-DD` at the start of `text`, or null. */
function parseDate(text: string): number | null {
  const year = digits(text, 0, 4);
  const month = digits(text, 5, 2);
  const day = digits(text, 8, 2);
  if (year === null || month === null || day === null) return null;
  if (year < 1 || month < 1 || month > 12) return null;
  if (day < 1 || day > daysInMonth(year, month)) return null;
  return daysFromCivil(year, month, day);
}

/** The seconds since midnight of `HH:MM:SS` at the start of `text`, or null. */
function parseTime(text: string): number | null {
  const hour = digits(text, 0, 2);
  const minute = digits(text, 3, 2);
  const second = digits(text, 6, 2);
  if (hour === null || minute === null || second === null) return null;
  if (hour > 23 || minute > 59 || second > 59) return null;
  return (hour * 60 + minute) * 60 + second;
}

/** The milliseconds of the fraction that starts at `start` (a `.`), and where it ends; null without a digit. */
function parseFraction(text: string, start: number): { ms: number; end: number } | null {
  let end = start + 1;
  while (end < text.length && isDigit(text.charCodeAt(end))) end++;
  if (end === start + 1) return null;
  // The first three digits are the milliseconds; a shorter fraction is padded on the right.
  const kept = Math.min(end - start - 1, 3);
  const scale = [1, 100, 10, 1][kept] ?? 1;
  return { ms: (digits(text, start + 1, kept) ?? 0) * scale, end };
}

const SIGNS: Readonly<Record<string, number>> = { "+": 1, "-": -1 };

/** The offset `Z`, `+HH:MM` or `-HH:MM` that is all of `text`, in minutes east of UTC; null otherwise. */
function parseOffset(text: string): number | null {
  if (text === "Z") return 0;
  const sign = SIGNS[text[0] ?? ""];
  if (sign === undefined || text.length !== 6 || text[3] !== ":") return null;
  const hour = digits(text, 1, 2);
  const minute = digits(text, 4, 2);
  if (hour === null || minute === null) return null;
  return hour > 23 || minute > 59 ? null : sign * (hour * 60 + minute);
}

/** The separators in place; the shortest form is "0001-01-01T00:00:00Z". */
function hasShape(text: string): boolean {
  return (
    text.length >= 20 &&
    text[4] === "-" &&
    text[7] === "-" &&
    text[10] === "T" &&
    text[13] === ":" &&
    text[16] === ":"
  );
}

/** `YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM)` as Unix milliseconds, or null. */
function parseInstant(text: string): number | null {
  if (!hasShape(text)) return null;
  const days = parseDate(text);
  const seconds = parseTime(text.slice(11));
  if (days === null || seconds === null) return null;
  const fraction = text[19] === "." ? parseFraction(text, 19) : { ms: 0, end: 19 };
  if (fraction === null) return null;
  const offsetMinutes = parseOffset(text.slice(fraction.end));
  if (offsetMinutes === null) return null;
  return days * 86_400_000 + seconds * 1000 + fraction.ms - offsetMinutes * 60_000;
}

/** A finite number is Unix milliseconds, a string is parsed, anything else is not an instant. */
export function toInstant(value: unknown): number | null {
  if (typeof value === "number") return Number.isFinite(value) ? value : null;
  return typeof value === "string" ? parseInstant(value) : null;
}
