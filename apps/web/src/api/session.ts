/** Sign-in is the Worker's POST /sign-in (`auth.server.ts`), because it runs with no session to check. */
import { createServerFn } from "@tanstack/react-start";
import { getRequest, setResponseHeader } from "@tanstack/react-start/server";
import { clearedSessionCookie } from "./auth.server.ts";

/** Ends this browser's session. Rotating the signing secret is what ends all of them. */
export const signOut = createServerFn({ method: "POST" }).handler(() => {
  setResponseHeader("set-cookie", clearedSessionCookie(getRequest().url));
});
