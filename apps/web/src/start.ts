import { createStart } from "@tanstack/react-start";
import { requireSession } from "./api/session-middleware.ts";

/** Picked up by TanStack Start by this file's name. `functionMiddleware` runs on every server function. */
export const startInstance = createStart(() => ({ functionMiddleware: [requireSession] }));
