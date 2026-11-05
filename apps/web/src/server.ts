import handler from "@tanstack/react-start/server-entry";
import { gate, type RequestContext } from "./api/auth.server.ts";
import { authSecrets } from "./api/env.server.ts";
import { createNonce, withSecurityHeaders } from "./api/security-headers.server.ts";

declare module "@tanstack/react-start" {
  interface Register {
    server: { requestContext: RequestContext & { nonce: string } };
  }
}

/**
 * The Worker's entry: the sign-in gate, then TanStack Start's `fetch`, with the security headers on every answer.
 * The nonce goes to the policy and into the request context, where `getRouter` reads it for the inline scripts.
 */
export default {
  fetch: (request: Request): Promise<Response> => {
    const nonce = createNonce();
    return withSecurityHeaders(nonce, () =>
      gate(request, authSecrets(), Date.now(), (context) =>
        handler.fetch(request, { context: { ...context, nonce } }),
      ),
    );
  },
};
