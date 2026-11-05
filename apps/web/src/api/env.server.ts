import { env } from "cloudflare:workers";
import { createAdminApi, readConnection, type AdminApi } from "./client.server.ts";

/** The admin API client for this request, or why there is none. The only place the token is read. */
export function adminApi(): { ok: true; api: AdminApi } | { ok: false; message: string } {
  const read = readConnection(
    Reflect.get(env, "FEATURESLASH_API_URL"),
    Reflect.get(env, "FEATURESLASH_ADMIN_TOKEN"),
  );
  if (!read.ok) return read;
  return { ok: true, api: createAdminApi(read.connection, fetch) };
}
