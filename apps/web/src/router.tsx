import { ErrorPage, NotFound } from "@hackcove/ui-react";
import {
  createRouter as createTanStackRouter,
  type ErrorComponentProps,
} from "@tanstack/react-router";
import { getGlobalStartContext } from "@tanstack/react-start";
import { routeTree } from "./routeTree.gen";

function RouteError({ reset }: ErrorComponentProps) {
  return <ErrorPage onRetry={reset} renderHome={(props) => <a href="/" {...props} />} />;
}

export function getRouter() {
  // Set on the server by `src/server.ts`; the browser hydrates what was written and has none.
  const nonce = getGlobalStartContext()?.nonce;
  return createTanStackRouter({
    routeTree,
    ...(nonce === undefined ? {} : { ssr: { nonce } }),
    scrollRestoration: true,
    defaultNotFoundComponent: () => <NotFound renderHome={(props) => <a href="/" {...props} />} />,
    defaultErrorComponent: RouteError,
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof getRouter>;
  }
}
