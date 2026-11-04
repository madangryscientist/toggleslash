import { SITE_URL } from "./links.ts";

/** The canonical URL of a page: the product domain plus its path (no trailing slash except the home page). */
export function canonicalUrl(path: string): string {
  return path === "/" ? `${SITE_URL}/` : `${SITE_URL}${path}`;
}
