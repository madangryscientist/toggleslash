import { type LinkAdapter, LinkProvider } from "@hackcove/ui-react";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";

// Every hackcove part that links takes a plain `href`; this draws an in-app one through the router.
// `to="."` satisfies the typed `to`, and the `href` wins.
const routerLink: LinkAdapter = (props) => <Link to="." {...props} />;

/** The router's `InnerWrap`: hackcove links navigate through TanStack Router. */
export function LinkThroughRouter({ children }: { children: ReactNode }) {
  return <LinkProvider link={routerLink}>{children}</LinkProvider>;
}
