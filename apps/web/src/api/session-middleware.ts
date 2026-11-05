/**
 * The dashboard's protection. `src/start.ts` registers this as global function middleware, so it runs
 * before the validator and the handler of every `createServerFn`, including any added later, on a call
 * from the browser and on one made while rendering a page.
 */
import { createMiddleware } from "@tanstack/react-start";
import { getRequest } from "@tanstack/react-start/server";
import { hasSession } from "./auth.server.ts";
import { authSecrets } from "./env.server.ts";
import { signInPath } from "./return-path.ts";

export const requireSession = createMiddleware({ type: "function" })
  .client(async ({ next }) => {
    try {
      return await next();
    } catch (error) {
      // The session ran out while the page was open: a whole-page load, which the Worker sends to sign-in.
      if (error instanceof Response && error.status === 401 && typeof window !== "undefined") {
        const { pathname, search } = window.location;
        window.location.assign(signInPath.to(`${pathname}${search}`));
      }
      throw error;
    }
  })
  .server(async ({ next }) => {
    if (!(await hasSession(getRequest(), authSecrets(), Date.now()))) {
      throw new Response("Sign in first.", {
        status: 401,
        headers: { "content-type": "text/plain" },
      });
    }
    return next();
  });
