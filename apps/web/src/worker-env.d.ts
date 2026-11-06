/**
 * The Worker's secrets. They are not in `wrangler.jsonc`, so `wrangler types` cannot generate them into
 * `worker-configuration.d.ts`; this merges into its `Cloudflare.Env`. Either may be unset.
 */
declare namespace Cloudflare {
  interface Env {
    TOGGLESLASH_OPERATOR_PASSWORD?: string;
    TOGGLESLASH_SESSION_SECRET?: string;
  }
}
