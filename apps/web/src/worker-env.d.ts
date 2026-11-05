/**
 * The Worker's secrets. They are not in `wrangler.jsonc`, so `wrangler types` cannot generate them into
 * `worker-configuration.d.ts`; this merges into its `Cloudflare.Env`. Either may be unset.
 */
declare namespace Cloudflare {
  interface Env {
    FEATURESLASH_OPERATOR_PASSWORD?: string;
    FEATURESLASH_SESSION_SECRET?: string;
  }
}
