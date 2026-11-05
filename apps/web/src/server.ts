import handler from "@tanstack/react-start/server-entry";
import { gate, type RequestContext } from "./api/auth.server.ts";
import { authSecrets } from "./api/env.server.ts";

declare module "@tanstack/react-start" {
  interface Register {
    server: { requestContext: RequestContext };
  }
}

/** The Worker's entry: the sign-in gate, then TanStack Start's `fetch`. */
export default {
  fetch: (request: Request): Promise<Response> =>
    gate(request, authSecrets(), Date.now(), (context) => handler.fetch(request, { context })),
};
