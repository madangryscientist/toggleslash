import { defaultStringifySearch } from "@tanstack/react-router";

/**
 * An in-app `href` with a search the way the router writes one, so a hackcove link (drawn through
 * the router's `LinkProvider`) carries the same query a typed `<Link search>` would. An `undefined`
 * entry is left out.
 */
export function appHref(path: string, search: Record<string, string | undefined> = {}): string {
  return path + defaultStringifySearch(search);
}

/** The flag detail page's `href` for one flag, in a project and environment. */
export function flagHref(flag: string, search: Record<string, string | undefined>): string {
  return appHref(`/flags/${encodeURIComponent(flag)}`, search);
}
