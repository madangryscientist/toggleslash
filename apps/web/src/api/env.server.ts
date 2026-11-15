import { env } from "cloudflare:workers";
import type { AuthSecrets } from "./auth.server.ts";
import { createAdminApi, readConnection, type AdminApi } from "./client.server.ts";

/** The sign-in secrets, for `auth.server.ts`. The only place they are read. */
export function authSecrets(): AuthSecrets {
  return {
    password: env.TOGGLESLASH_OPERATOR_PASSWORD,
    sessionSecret: env.TOGGLESLASH_SESSION_SECRET,
  };
}

/** The admin API client for this request, or why there is none. The only place the token and the Access service token are read. */
export function adminApi(): { ok: true; api: AdminApi } | { ok: false; message: string } {
  const read = readConnection({
    url: Reflect.get(env, "TOGGLESLASH_API_URL"),
    token: Reflect.get(env, "TOGGLESLASH_ADMIN_TOKEN"),
    accessClientId: Reflect.get(env, "TOGGLESLASH_ACCESS_CLIENT_ID"),
    accessClientSecret: Reflect.get(env, "TOGGLESLASH_ACCESS_CLIENT_SECRET"),
  });
  if (!read.ok) return read;
  return { ok: true, api: createAdminApi(read.connection, fetch) };
}
