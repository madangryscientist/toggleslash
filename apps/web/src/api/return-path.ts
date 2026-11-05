/**
 * Where sign-in sends you afterwards. It comes from the URL, so it is untrusted: only a path on this
 * origin passes, and everything else means the home page.
 */
const BASE = "http://featureslash.invalid";

export function safeReturnPath(value: unknown): string {
  if (typeof value !== "string" || !value.startsWith("/") || !URL.canParse(value, BASE)) return "/";
  // Judged as a browser reads it: `/\\evil.test` is another origin, and `/.//evil.test` becomes `//evil.test`.
  const url = new URL(value, BASE);
  if (url.origin !== BASE || url.pathname.startsWith("//")) return "/";
  return `${url.pathname}${url.search}${url.hash}`;
}

const query = (returnTo: unknown, extra = ""): string =>
  `/sign-in?${extra}returnTo=${encodeURIComponent(safeReturnPath(returnTo))}`;

export const signInPath = {
  to: (returnTo: unknown): string => query(returnTo),
  failed: (returnTo: unknown): string => query(returnTo, "failed=true&"),
  after: (returnTo: unknown): string => safeReturnPath(returnTo),
};
