/**
 * The Worker's secrets. They are not in `wrangler.jsonc`, so `wrangler types` cannot generate them into
 * `worker-configuration.d.ts`; this merges into its `Cloudflare.Env`. Any may be unset. The Access pair is a
 * Cloudflare Access service token for an admin API behind Access: both set, or neither.
 */
declare namespace Cloudflare {
  interface Env {
    TOGGLESLASH_OPERATOR_PASSWORD?: string;
    TOGGLESLASH_SESSION_SECRET?: string;
    TOGGLESLASH_ACCESS_CLIENT_ID?: string;
    TOGGLESLASH_ACCESS_CLIENT_SECRET?: string;
  }
}
